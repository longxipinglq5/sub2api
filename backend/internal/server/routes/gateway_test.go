package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGatewayRoutesTestRouter(platform ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	groupPlatform := service.PlatformOpenAI
	if len(platform) > 0 && platform[0] != "" {
		groupPlatform = platform[0]
	}

	RegisterGatewayRoutes(
		router,
		&handler.Handlers{
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupID := int64(1)
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				GroupID: &groupID,
				Group:   &service.Group{Platform: groupPlatform},
			})
			c.Next()
		}),
		nil,
		nil,
		nil,
		nil,
		&config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1 << 20}},
	)

	return router
}

func newGatewayRoutesTestRouterWithAPIKey(apiKey *service.APIKey, maxBodySize ...int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	bodyLimit := int64(1 << 20)
	if len(maxBodySize) > 0 {
		bodyLimit = maxBodySize[0]
	}

	RegisterGatewayRoutes(
		router,
		&handler.Handlers{
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyAPIKey), apiKey)
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 1, Concurrency: 1})
			c.Next()
		}),
		nil,
		nil,
		nil,
		nil,
		&config.Config{Gateway: config.GatewayConfig{MaxBodySize: bodyLimit}},
	)

	return router
}

func TestGatewayRoutesOpenAIResponsesCompactPathIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/responses/compact",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI responses handler", path)
	}
}

func TestGatewayRoutesOpenAIImagesPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-image-2","prompt":"draw a cat"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI images handler", path)
	}
}

func TestGatewayRoutesGrokOnlyAllowsResponsesHTTP(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformGrok)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/messages"},
		{http.MethodPost, "/v1/chat/completions"},
		{http.MethodPost, "/chat/completions"},
		{http.MethodGet, "/v1/responses"},
		{http.MethodGet, "/responses"},
		{http.MethodGet, "/backend-api/codex/responses"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"model":"grok"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "method=%s path=%s", tc.method, tc.path)
		require.Contains(t, w.Body.String(), "not supported for Grok groups")
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"grok","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "Token counting is not supported for this platform")

	for _, path := range []string{
		"/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"grok","input":"hi"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should still reach Responses handler", path)
	}
}

func TestGatewayRoutesOpenAICountTokensPathIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformOpenAI)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)
	require.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestGatewayRoutes_QwenImages(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformQwen)

	for _, path := range []string{
		"/v1/images/generations",
		"/images/generations",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"qwen-image","prompt":"draw a cat"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should reach image generation handler", path)
	}

	for _, path := range []string{
		"/v1/images/edits",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"qwen-image","prompt":"draw a cat"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
		errorObj, ok := payload["error"].(map[string]any)
		require.True(t, ok, "error object missing for path=%s", path)
		require.Equal(t, "not_found_error", errorObj["type"])
		message, _ := errorObj["message"].(string)
		require.NotEmpty(t, message)
	}
}

func TestAPIKeyHasGroupPlatform_IncludesSecondaryGroups(t *testing.T) {
	primaryGroupID := int64(5)
	apiKey := &service.APIKey{
		GroupID: &primaryGroupID,
		Group:   &service.Group{ID: 5, Platform: service.PlatformQwen},
		Groups: []service.Group{
			{ID: 5, Platform: service.PlatformQwen},
			{ID: 2, Platform: service.PlatformOpenAI},
		},
	}

	require.True(t, apiKeyHasGroupPlatform(apiKey, service.PlatformOpenAI))
	require.True(t, apiKeyHasGroupPlatform(apiKey, service.PlatformQwen))
	require.False(t, apiKeyHasGroupPlatform(apiKey, service.PlatformGemini))
}

func TestGatewayRoutesChatCompletions_UsesOpenAIHandlerWhenSecondaryGroupIsOpenAI(t *testing.T) {
	primaryGroupID := int64(5)
	router := newGatewayRoutesTestRouterWithAPIKey(&service.APIKey{
		ID:       1,
		GroupID:  &primaryGroupID,
		GroupIDs: []int64{5, 2},
		Group:    &service.Group{ID: 5, Platform: service.PlatformQwen},
		Groups: []service.Group{
			{ID: 5, Platform: service.PlatformQwen},
			{ID: 2, Platform: service.PlatformOpenAI},
		},
		User: &service.User{ID: 1, Status: service.StatusActive},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "Service temporarily unavailable")
}

func TestGatewayRoutesChatCompletions_ModelPeekReturns413WhenBodyTooLarge(t *testing.T) {
	primaryGroupID := int64(5)
	router := newGatewayRoutesTestRouterWithAPIKey(&service.APIKey{
		ID:       1,
		GroupID:  &primaryGroupID,
		GroupIDs: []int64{5, 2},
		Group:    &service.Group{ID: 5, Platform: service.PlatformQwen},
		Groups: []service.Group{
			{ID: 5, Platform: service.PlatformQwen},
			{ID: 2, Platform: service.PlatformOpenAI},
		},
		User: &service.User{ID: 1, Status: service.StatusActive},
	}, 8)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.5"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestShouldUseOpenAIChatCompletionsHandler_RoutesByPrimaryOrModel(t *testing.T) {
	require.True(t, shouldUseOpenAIChatCompletionsHandler(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformOpenAI},
	}, "claude-sonnet-4-5"))

	require.True(t, shouldUseOpenAIChatCompletionsHandler(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformQwen},
		Groups: []service.Group{
			{Platform: service.PlatformQwen},
			{Platform: service.PlatformOpenAI},
		},
	}, "gpt-5.5"))

	require.False(t, shouldUseOpenAIChatCompletionsHandler(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformAnthropic},
		Groups: []service.Group{
			{Platform: service.PlatformAnthropic},
			{Platform: service.PlatformOpenAI},
		},
	}, "claude-sonnet-4-5"))
}

func TestShouldUseOpenAIResponsesHandler_RoutesByPrimaryOrModel(t *testing.T) {
	require.True(t, shouldUseOpenAIResponsesHandler(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformOpenAI},
	}, "claude-sonnet-4-5"))

	require.True(t, shouldUseOpenAIResponsesHandler(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformQwen},
		Groups: []service.Group{
			{Platform: service.PlatformQwen},
			{Platform: service.PlatformOpenAI},
		},
	}, "gpt-5.5"))

	require.True(t, shouldUseOpenAIResponsesHandler(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformAnthropic},
		Groups: []service.Group{
			{Platform: service.PlatformAnthropic},
			{Platform: service.PlatformGrok},
		},
	}, "grok-4"))

	require.False(t, shouldUseOpenAIResponsesHandler(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformAnthropic},
		Groups: []service.Group{
			{Platform: service.PlatformAnthropic},
			{Platform: service.PlatformOpenAI},
		},
	}, "claude-sonnet-4-5"))
}
