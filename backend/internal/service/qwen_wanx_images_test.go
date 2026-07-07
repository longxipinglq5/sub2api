package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardQwenWanxImages_SuccessURLCase(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var submitCount atomic.Int32
	var pollCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/services/aigc/text2image/image-synthesis":
			submitCount.Add(1)
			require.Equal(t, "Bearer dashscope-key", r.Header.Get("Authorization"))
			require.Equal(t, "enable", r.Header.Get("X-DashScope-Async"))
			require.Equal(t, "application/json", r.Header.Get("Content-Type"))

			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.Equal(t, "wanx2.1-t2i-plus", gjson.GetBytes(body, "model").String())
			require.Equal(t, "draw a skyline", gjson.GetBytes(body, "input.prompt").String())
			require.Equal(t, "1024x1024", gjson.GetBytes(body, "parameters.size").String())
			require.Equal(t, int64(2), gjson.GetBytes(body, "parameters.n").Int())

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"req-submit","output":{"task_id":"task-123","task_status":"PENDING"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/task-123":
			count := pollCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if count == 1 {
				_, _ = w.Write([]byte(`{"request_id":"req-poll-1","output":{"task_id":"task-123","task_status":"RUNNING"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"request_id":"req-poll-2","output":{"task_id":"task-123","task_status":"SUCCEEDED","results":[{"url":"https://img.example/1.png"},{"url":"https://img.example/2.png"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := []byte(`{"model":"wanx2.1-t2i-plus","prompt":"draw a skyline","n":2,"size":"1024x1024"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       7,
		Type:     AccountTypeAPIKey,
		Platform: PlatformQwen,
		Credentials: map[string]any{
			"api_key":          "dashscope-key",
			"base_url":         server.URL,
			"poll_interval_ms": 1,
			"poll_timeout_ms":  2000,
		},
	}

	result, err := svc.forwardQwenWanxImages(context.Background(), c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, int32(1), submitCount.Load())
	require.GreaterOrEqual(t, pollCount.Load(), int32(2))
	require.Equal(t, http.StatusOK, rec.Code)

	var payload struct {
		Created int64 `json:"created"`
		Data    []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Positive(t, payload.Created)
	require.Len(t, payload.Data, 2)
	require.Equal(t, "https://img.example/1.png", payload.Data[0].URL)
	require.Equal(t, "https://img.example/2.png", payload.Data[1].URL)

	require.Equal(t, "req-poll-2", result.RequestID)
	require.Equal(t, "wanx2.1-t2i-plus", result.Model)
	require.Equal(t, "wanx2.1-t2i-plus", result.BillingModel)
	require.Equal(t, "wanx2.1-t2i-plus", result.UpstreamModel)
	require.Equal(t, 2, result.ImageCount)
	require.Equal(t, ImageBillingSize1K, result.ImageSize)
	require.Equal(t, ImageSizeSourceInput, result.ImageSizeSource)
	require.Greater(t, result.Duration, time.Duration(0))
}

func TestForwardQwenWanxImages_MissingAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"wanx2.1-t2i-turbo","prompt":"draw a cat"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{Credentials: map[string]any{}}
	result, err := svc.forwardQwenWanxImages(context.Background(), c, account, parsed)
	require.Nil(t, result)
	require.EqualError(t, err, "missing DashScope api_key")
}

func TestForwardQwenWanxImages_TaskFailedReturnsFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/services/aigc/text2image/image-synthesis":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"req-submit","output":{"task_id":"task-err","task_status":"PENDING"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/task-err":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"req-poll-failed","output":{"task_id":"task-err","task_status":"FAILED"},"code":"TaskFailed","message":"generation failed"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := []byte(`{"model":"wanx2.1-t2i-turbo","prompt":"draw a cat"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		Credentials: map[string]any{
			"api_key":          "dashscope-key",
			"base_url":         server.URL,
			"poll_interval_ms": 1,
			"poll_timeout_ms":  2000,
		},
	}

	result, err := svc.forwardQwenWanxImages(context.Background(), c, account, parsed)
	require.Nil(t, result)
	require.Error(t, err)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), `"task_status":"FAILED"`)
}

func TestForwardQwenWanxImages_TimeoutDoesNotWaitFullInterval(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/services/aigc/text2image/image-synthesis":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"req-submit","output":{"task_id":"task-running","task_status":"PENDING"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/task-running":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"req-poll-running","output":{"task_id":"task-running","task_status":"RUNNING"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := []byte(`{"model":"wanx2.1-t2i-turbo","prompt":"draw a cat"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		Credentials: map[string]any{
			"api_key":          "dashscope-key",
			"base_url":         server.URL,
			"poll_interval_ms": 200,
			"poll_timeout_ms":  20,
		},
	}

	start := time.Now()
	result, err := svc.forwardQwenWanxImages(context.Background(), c, account, parsed)
	elapsed := time.Since(start)

	require.Nil(t, result)
	require.Error(t, err)
	require.Less(t, elapsed, 150*time.Millisecond)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
}

func TestForwardQwenWanxImages_TaskFailedWithOutputCodeMessageReturnsFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/services/aigc/text2image/image-synthesis":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"req-submit","output":{"task_id":"task-err","task_status":"PENDING"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/task-err":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"req-poll-failed","output":{"task_id":"task-err","task_status":"FAILED","code":"InvalidParameter","message":"bad prompt"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := []byte(`{"model":"wanx2.1-t2i-turbo","prompt":"draw a cat"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		Credentials: map[string]any{
			"api_key":          "dashscope-key",
			"base_url":         server.URL,
			"poll_interval_ms": 1,
			"poll_timeout_ms":  2000,
		},
	}

	result, err := svc.forwardQwenWanxImages(context.Background(), c, account, parsed)
	require.Nil(t, result)
	require.Error(t, err)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), `InvalidParameter`)
	require.Contains(t, string(failoverErr.ResponseBody), `bad prompt`)
}
