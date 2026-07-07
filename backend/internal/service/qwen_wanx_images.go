package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	dashScopeDefaultBaseURL       = "https://dashscope.aliyuncs.com/api/v1"
	dashScopeDefaultPollInterval  = 1000 * time.Millisecond
	dashScopeDefaultPollTimeout   = 120 * time.Second
	dashScopeImageSynthesisPath   = "/services/aigc/text2image/image-synthesis"
	dashScopeTaskStatusPathPrefix = "/tasks/"
)

type dashScopeImageSubmitRequest struct {
	Model      string                      `json:"model"`
	Input      dashScopeImagePromptInput   `json:"input"`
	Parameters dashScopeImageTaskParameter `json:"parameters"`
}

type dashScopeImagePromptInput struct {
	Prompt string `json:"prompt"`
}

type dashScopeImageTaskParameter struct {
	Size string `json:"size"`
	N    int    `json:"n"`
}

type dashScopeImageTaskResponse struct {
	RequestID string `json:"request_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Output    struct {
		TaskID     string `json:"task_id"`
		TaskStatus string `json:"task_status"`
		Code       string `json:"code"`
		Message    string `json:"message"`
		Results    []struct {
			URL string `json:"url"`
		} `json:"results"`
	} `json:"output"`
}

type openAIImageURLResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		URL string `json:"url"`
	} `json:"data"`
}

func (s *OpenAIGatewayService) forwardQwenWanxImages(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	parsed *OpenAIImagesRequest,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil {
		return nil, fmt.Errorf("missing account")
	}
	if parsed == nil {
		return nil, fmt.Errorf("parsed images request is required")
	}

	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" {
		return nil, fmt.Errorf("missing DashScope api_key")
	}

	baseURL, err := normalizeDashScopeBaseURL(account.GetCredential("base_url"))
	if err != nil {
		return nil, err
	}
	pollInterval := dashScopeDurationCredential(account, "poll_interval_ms", dashScopeDefaultPollInterval)
	pollTimeout := dashScopeDurationCredential(account, "poll_timeout_ms", dashScopeDefaultPollTimeout)

	requestModel := strings.TrimSpace(parsed.Model)
	upstreamModel := account.GetMappedModel(requestModel)
	if strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = requestModel
	}

	submitBody := dashScopeImageSubmitRequest{
		Model: upstreamModel,
		Input: dashScopeImagePromptInput{
			Prompt: parsed.Prompt,
		},
		Parameters: dashScopeImageTaskParameter{
			Size: parsed.Size,
			N:    parsed.N,
		},
	}
	resp, respBody, err := dashScopeDoJSON(ctx, http.MethodPost, buildDashScopeURL(baseURL, dashScopeImageSynthesisPath), apiKey, submitBody)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, &UpstreamFailoverError{
			StatusCode:   resp.StatusCode,
			ResponseBody: respBody,
		}
	}

	var submit dashScopeImageTaskResponse
	if err := json.Unmarshal(respBody, &submit); err != nil {
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: respBody}
	}
	taskID := strings.TrimSpace(submit.Output.TaskID)
	if taskID == "" {
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: respBody}
	}

	timeout := time.NewTimer(pollTimeout)
	defer stopAndDrainTimer(timeout)
	var finalTask dashScopeImageTaskResponse
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout.C:
			return nil, &UpstreamFailoverError{
				StatusCode:   http.StatusBadGateway,
				ResponseBody: []byte(`{"message":"DashScope task poll timeout"}`),
			}
		default:
		}

		pollResp, pollBody, err := dashScopeDoJSON(ctx, http.MethodGet, buildDashScopeURL(baseURL, dashScopeTaskStatusPathPrefix+taskID), apiKey, nil)
		if err != nil {
			return nil, err
		}
		_ = pollResp.Body.Close()
		if pollResp.StatusCode >= http.StatusBadRequest {
			return nil, &UpstreamFailoverError{
				StatusCode:   pollResp.StatusCode,
				ResponseBody: pollBody,
			}
		}
		if err := json.Unmarshal(pollBody, &finalTask); err != nil {
			return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: pollBody}
		}

		switch strings.ToUpper(strings.TrimSpace(finalTask.Output.TaskStatus)) {
		case "SUCCEEDED":
			urls := make([]string, 0, len(finalTask.Output.Results))
			for _, item := range finalTask.Output.Results {
				if trimmed := strings.TrimSpace(item.URL); trimmed != "" {
					urls = append(urls, trimmed)
				}
			}
			if len(urls) == 0 {
				return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: pollBody}
			}

			responsePayload := openAIImageURLResponse{
				Created: time.Now().Unix(),
				Data: make([]struct {
					URL string `json:"url"`
				}, 0, len(urls)),
			}
			for _, imageURL := range urls {
				responsePayload.Data = append(responsePayload.Data, struct {
					URL string `json:"url"`
				}{URL: imageURL})
			}
			c.JSON(http.StatusOK, responsePayload)

			result := &OpenAIForwardResult{
				RequestID:      firstNonEmptyTrimmed(finalTask.RequestID, submit.RequestID),
				Model:          requestModel,
				BillingModel:   upstreamModel,
				UpstreamModel:  upstreamModel,
				Duration:       time.Since(startTime),
				ImageCount:     len(urls),
				ImageSize:      parsed.Size,
				ImageInputSize: parsed.Size,
			}
			ApplyOpenAIImageBillingResolution(result)
			return result, nil
		case "FAILED":
			return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: dashScopeTaskFailureBody(pollBody, finalTask)}
		}

		interval := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			stopAndDrainTimer(interval)
			return nil, ctx.Err()
		case <-timeout.C:
			stopAndDrainTimer(interval)
			return nil, &UpstreamFailoverError{
				StatusCode:   http.StatusBadGateway,
				ResponseBody: []byte(`{"message":"DashScope task poll timeout"}`),
			}
		case <-interval.C:
		}
	}
}

func normalizeDashScopeBaseURL(raw string) (string, error) {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = dashScopeDefaultBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid base_url: missing scheme or host")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func dashScopeDurationCredential(account *Account, key string, fallback time.Duration) time.Duration {
	if account == nil {
		return fallback
	}
	value := account.GetCredentialAsInt64(key)
	if value <= 0 {
		return fallback
	}
	return time.Duration(value) * time.Millisecond
}

func buildDashScopeURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}

func dashScopeDoJSON(ctx context.Context, method string, targetURL string, apiKey string, payload any) (*http.Response, []byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, err
		}
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		req.Header.Set("X-DashScope-Async", "enable")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	body, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		return nil, nil, err
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, body, nil
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func dashScopeTaskFailureBody(body []byte, task dashScopeImageTaskResponse) []byte {
	if len(body) > 0 {
		return body
	}
	failure := map[string]any{
		"output": map[string]any{
			"task_status": task.Output.TaskStatus,
		},
	}
	if trimmed := strings.TrimSpace(task.Output.Code); trimmed != "" {
		failure["output"].(map[string]any)["code"] = trimmed
	}
	if trimmed := strings.TrimSpace(task.Output.Message); trimmed != "" {
		failure["output"].(map[string]any)["message"] = trimmed
	}
	if trimmed := strings.TrimSpace(task.Code); trimmed != "" {
		failure["code"] = trimmed
	}
	if trimmed := strings.TrimSpace(task.Message); trimmed != "" {
		failure["message"] = trimmed
	}
	encoded, err := json.Marshal(failure)
	if err != nil {
		return []byte(`{"message":"DashScope task failed"}`)
	}
	return encoded
}

func stopAndDrainTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
