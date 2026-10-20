package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type OpenAIRealtimeTurnResult struct {
	RequestID    string
	Usage        OpenAIUsage
	Duration     time.Duration
	FirstTokenMs *int
	TerminalType string
}

type OpenAIRealtimeHooks struct {
	OnTurnComplete func(OpenAIRealtimeTurnResult)
}

type openAIRealtimeInitialError struct {
	statusCode int
	payload    []byte
}

func (e *openAIRealtimeInitialError) Error() string {
	if e == nil {
		return "openai realtime initial error"
	}
	return fmt.Sprintf("openai realtime initial error: status=%d", e.statusCode)
}

func isOpenAIRealtimeTransientError(payload []byte, status int) bool {
	if status == http.StatusTooManyRequests || status >= http.StatusInternalServerError {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "error.message").String()))
	code := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "error.code").String()))
	for _, value := range []string{message, code} {
		for _, needle := range []string{"at capacity", "overloaded", "temporarily unavailable", "try again later", "server is busy", "capacity"} {
			if strings.Contains(value, needle) {
				return true
			}
		}
	}
	return false
}

type openAIRealtimeModelFrameConn struct {
	inner          openaiwsv2.FrameConn
	requestedModel string
	mappedModel    string
}

func (c *openAIRealtimeModelFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	msgType, payload, err := c.inner.ReadFrame(ctx)
	if err != nil || msgType != coderws.MessageText || len(payload) == 0 || !gjson.ValidBytes(payload) {
		return msgType, payload, err
	}
	frameType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	var path string
	switch frameType {
	case "session.update":
		path = "session.model"
	case "response.create":
		path = "response.model"
		if !gjson.GetBytes(payload, path).Exists() {
			path = "model"
		}
	}
	if path == "" || !gjson.GetBytes(payload, path).Exists() {
		return msgType, payload, nil
	}
	model := strings.TrimSpace(gjson.GetBytes(payload, path).String())
	if model != "" && !strings.EqualFold(model, c.requestedModel) && !strings.EqualFold(model, c.mappedModel) {
		return msgType, nil, NewOpenAIWSClientCloseError(
			coderws.StatusPolicyViolation,
			"realtime session model cannot be changed after connection",
			nil,
		)
	}
	updated, updateErr := sjson.SetBytes(payload, path, c.mappedModel)
	if updateErr != nil {
		return msgType, nil, updateErr
	}
	return msgType, updated, nil
}

func (c *openAIRealtimeModelFrameConn) WriteFrame(ctx context.Context, msgType coderws.MessageType, payload []byte) error {
	if msgType == coderws.MessageText && len(payload) > 0 && gjson.ValidBytes(payload) && c.requestedModel != c.mappedModel {
		for _, path := range []string{"session.model", "response.model", "model"} {
			value := strings.TrimSpace(gjson.GetBytes(payload, path).String())
			if value == "" || !strings.EqualFold(value, c.mappedModel) {
				continue
			}
			if updated, err := sjson.SetBytes(payload, path, c.requestedModel); err == nil {
				payload = updated
			}
		}
	}
	return c.inner.WriteFrame(ctx, msgType, payload)
}

func (c *openAIRealtimeModelFrameConn) Close() error {
	if c == nil || c.inner == nil {
		return nil
	}
	return c.inner.Close()
}

func (s *OpenAIGatewayService) buildOpenAIRealtimeWSURL(account *Account, model string, incoming url.Values) (string, error) {
	if account == nil || !account.IsOpenAIApiKey() {
		return "", errors.New("realtime requires an OpenAI API key account")
	}
	baseURL, err := s.validateUpstreamBaseURL(account.GetOpenAIBaseURL())
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid realtime base url: %w", err)
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/responses") {
		path = strings.TrimSuffix(path, "/responses")
	}
	lastSegment := path
	if slash := strings.LastIndex(lastSegment, "/"); slash >= 0 {
		lastSegment = lastSegment[slash+1:]
	}
	if isOpenAIAPIVersionSegment(lastSegment) {
		parsed.Path = strings.TrimRight(path, "/") + "/realtime"
	} else {
		parsed.Path = strings.TrimRight(path, "/") + "/v1/realtime"
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("unsupported realtime websocket scheme: %s", parsed.Scheme)
	}
	query := parsed.Query()
	for key, values := range incoming {
		if strings.EqualFold(key, "key") || strings.EqualFold(key, "api_key") || strings.EqualFold(key, "access_token") || strings.EqualFold(key, "token") || strings.EqualFold(key, "authorization") || strings.EqualFold(key, "model") {
			continue
		}
		query.Del(key)
		for _, value := range values {
			query.Add(key, value)
		}
	}
	query.Set("model", strings.TrimSpace(model))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func buildOpenAIRealtimeWSHeaders(c *gin.Context, account *Account, token string) http.Header {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	headers.Set("OpenAI-Beta", "realtime=v1")
	if account != nil {
		if userAgent := strings.TrimSpace(account.GetOpenAIUserAgent()); userAgent != "" {
			headers.Set("User-Agent", userAgent)
		}
	}
	if headers.Get("User-Agent") == "" && c != nil {
		if userAgent := strings.TrimSpace(c.GetHeader("User-Agent")); userAgent != "" {
			headers.Set("User-Agent", userAgent)
		}
	}
	return headers
}

func (s *OpenAIGatewayService) ProxyRealtimeWebSocket(
	ctx context.Context,
	c *gin.Context,
	clientConn *coderws.Conn,
	account *Account,
	token string,
	requestedModel string,
	upstreamModel string,
	hooks *OpenAIRealtimeHooks,
) error {
	if s == nil || clientConn == nil || account == nil {
		return errors.New("invalid realtime websocket proxy arguments")
	}
	var incoming url.Values
	if c != nil && c.Request != nil && c.Request.URL != nil {
		incoming = c.Request.URL.Query()
	}
	wsURL, err := s.buildOpenAIRealtimeWSURL(account, upstreamModel, incoming)
	if err != nil {
		return err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	dialCtx, cancelDial := context.WithTimeout(ctx, s.openAIWSDialTimeout())
	upstreamConn, statusCode, responseHeaders, dialErr := s.getOpenAIWSPassthroughDialer().Dial(
		dialCtx,
		wsURL,
		buildOpenAIRealtimeWSHeaders(c, account, token),
		proxyURL,
	)
	cancelDial()
	if dialErr != nil {
		if statusCode == 0 {
			statusCode = http.StatusServiceUnavailable
		}
		return &UpstreamFailoverError{
			StatusCode:      statusCode,
			ResponseHeaders: cloneHeader(responseHeaders),
		}
	}
	defer func() { _ = upstreamConn.Close() }()
	upstreamFrames, ok := upstreamConn.(openaiwsv2.FrameConn)
	if !ok {
		return errors.New("realtime upstream does not support frame relay")
	}
	clientFrames := &openAIRealtimeModelFrameConn{
		inner:          &openAIWSClientFrameConn{conn: clientConn},
		requestedModel: requestedModel,
		mappedModel:    upstreamModel,
	}
	result, relayExit := openaiwsv2.Relay(
		ctx,
		clientFrames,
		upstreamFrames,
		nil,
		openaiwsv2.RelayOptions{
			FirstMessageSent:                true,
			StartClientAfterFirstDownstream: true,
			WriteTimeout:                    s.openAIWSWriteTimeout(),
			IdleTimeout:                     s.openAIWSPassthroughIdleTimeout(),
			BeforeWriteClient: func(msgType coderws.MessageType, payload []byte, wroteDownstream bool) error {
				if wroteDownstream || msgType != coderws.MessageText || !gjson.ValidBytes(payload) || gjson.GetBytes(payload, "type").String() != "error" {
					return nil
				}
				status := openAIWSErrorHTTPStatus(payload)
				if isOpenAIRealtimeTransientError(payload, status) {
					return &openAIRealtimeInitialError{statusCode: status, payload: append([]byte(nil), payload...)}
				}
				return nil
			},
			OnTurnComplete: func(turn openaiwsv2.RelayTurnResult) {
				if hooks == nil || hooks.OnTurnComplete == nil {
					return
				}
				hooks.OnTurnComplete(OpenAIRealtimeTurnResult{
					RequestID: turn.RequestID,
					Usage: OpenAIUsage{
						InputTokens:               turn.Usage.InputTokens,
						AudioInputTokens:          turn.Usage.AudioInputTokens,
						OutputTokens:              turn.Usage.OutputTokens,
						AudioOutputTokens:         turn.Usage.AudioOutputTokens,
						CacheCreationInputTokens:  turn.Usage.CacheCreationInputTokens,
						CacheReadInputTokens:      turn.Usage.CacheReadInputTokens,
						CacheReadAudioInputTokens: turn.Usage.CacheReadAudioInputTokens,
						ImageOutputTokens:         turn.Usage.ImageOutputTokens,
					},
					Duration:     turn.Duration,
					FirstTokenMs: turn.FirstTokenMs,
					TerminalType: turn.TerminalEventType,
				})
			},
		},
	)
	if relayExit == nil {
		if result.UpstreamToClientFrames == 0 {
			return &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}
		}
		return nil
	}
	var initialErr *openAIRealtimeInitialError
	if !relayExit.WroteDownstream && errors.As(relayExit.Err, &initialErr) {
		return &UpstreamFailoverError{StatusCode: initialErr.statusCode, ResponseBody: initialErr.payload}
	}
	if !relayExit.WroteDownstream {
		return &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}
	}
	return fmt.Errorf("realtime websocket relay %s: %w", relayExit.Stage, relayExit.Err)
}
