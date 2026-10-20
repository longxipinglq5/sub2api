package routes

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// RegisterGatewayRoutes 注册 API 网关路由（Claude/OpenAI/Gemini 兼容）
func RegisterGatewayRoutes(
	r *gin.Engine,
	h *handler.Handlers,
	apiKeyAuth middleware.APIKeyAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	cfg *config.Config,
) {
	bodyLimit := middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize)
	clientRequestID := middleware.ClientRequestID()
	opsErrorLogger := handler.OpsErrorLoggerMiddleware(opsService)
	endpointNorm := handler.InboundEndpointMiddleware()

	// 未分组 Key 拦截中间件（按协议格式区分错误响应）
	requireGroupAnthropic := middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter)
	requireGroupGoogle := middleware.RequireGroupAssignment(settingService, middleware.GoogleErrorWriter)

	isOpenAIResponsesCompatibleGatewayPlatform := func(c *gin.Context) bool {
		apiKey, _ := middleware.GetAPIKeyFromContext(c)
		return shouldUseOpenAIResponsesHandler(apiKey, peekOpenAICompatibleRequestModel(c))
	}
	isOpenAIGatewayPlatform := func(c *gin.Context) bool {
		return getGroupPlatform(c) == service.PlatformOpenAI
	}
	hasOpenAIGroup := func(c *gin.Context) bool {
		apiKey, _ := middleware.GetAPIKeyFromContext(c)
		return apiKeyHasGroupPlatform(apiKey, service.PlatformOpenAI)
	}
	isOpenAIOrQwenImagesPlatform := func(c *gin.Context) bool {
		apiKey, _ := middleware.GetAPIKeyFromContext(c)
		switch {
		case apiKeyHasGroupPlatform(apiKey, service.PlatformOpenAI), apiKeyHasGroupPlatform(apiKey, service.PlatformQwen):
			return true
		default:
			return false
		}
	}
	rejectGrokUnsupportedEndpoint := func(c *gin.Context, endpoint string) {
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found_error",
				"message": endpoint + " is not supported for Grok groups",
			},
		})
	}
	rejectQwenImagesEditsUnsupported := func(c *gin.Context) {
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found_error",
				"message": "Image edits are not supported for Qwen groups",
			},
		})
	}
	handleImagesGenerations := func(c *gin.Context) {
		if !isOpenAIOrQwenImagesPlatform(c) {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Images API is not supported for this platform",
				},
			})
			return
		}
		h.OpenAIGateway.Images(c)
	}
	handleImagesEdits := func(c *gin.Context) {
		apiKey, _ := middleware.GetAPIKeyFromContext(c)
		switch {
		case apiKeyHasGroupPlatform(apiKey, service.PlatformOpenAI):
			h.OpenAIGateway.Images(c)
		case apiKeyHasGroupPlatform(apiKey, service.PlatformQwen):
			rejectQwenImagesEditsUnsupported(c)
		default:
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Images API is not supported for this platform",
				},
			})
		}
	}

	// API网关（Claude API兼容）
	gateway := r.Group("/v1")
	gateway.Use(bodyLimit)
	gateway.Use(clientRequestID)
	gateway.Use(opsErrorLogger)
	gateway.Use(endpointNorm)
	gateway.Use(gin.HandlerFunc(apiKeyAuth))
	gateway.Use(requireGroupAnthropic)
	{
		// /v1/messages: auto-route based on group platform
		gateway.POST("/messages", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformGrok {
				rejectGrokUnsupportedEndpoint(c, "Messages API")
				return
			}
			if isOpenAIGatewayPlatform(c) {
				h.OpenAIGateway.Messages(c)
				return
			}
			h.Gateway.Messages(c)
		})
		// /v1/messages/count_tokens: OpenAI uses Anthropic-compat bridge; other
		// OpenAI-compatible platforms keep the prior unsupported response.
		gateway.POST("/messages/count_tokens", func(c *gin.Context) {
			if isOpenAIGatewayPlatform(c) {
				h.OpenAIGateway.CountTokens(c)
				return
			}
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
				c.JSON(http.StatusNotFound, gin.H{
					"type": "error",
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Token counting is not supported for this platform",
					},
				})
				return
			}
			h.Gateway.CountTokens(c)
		})
		gateway.GET("/models", h.Gateway.Models)
		gateway.GET("/usage", h.Gateway.Usage)
		// OpenAI Responses API: auto-route based on group platform
		gateway.POST("/responses", func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		gateway.POST("/responses/*subpath", func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		gateway.GET("/responses", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformGrok {
				rejectGrokUnsupportedEndpoint(c, "Responses WebSocket API")
				return
			}
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
		gateway.GET("/realtime", func(c *gin.Context) {
			h.OpenAIGateway.RealtimeWebSocket(c)
		})
		// OpenAI Chat Completions API: auto-route based on group platform
		gateway.POST("/chat/completions", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformGrok {
				rejectGrokUnsupportedEndpoint(c, "Chat Completions API")
				return
			}
			apiKey, _ := middleware.GetAPIKeyFromContext(c)
			if shouldUseOpenAIChatCompletionsHandler(apiKey, peekOpenAICompatibleRequestModel(c)) {
				h.OpenAIGateway.ChatCompletions(c)
				return
			}
			h.Gateway.ChatCompletions(c)
		})
		gateway.POST("/embeddings", func(c *gin.Context) {
			if !hasOpenAIGroup(c) {
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
				c.JSON(http.StatusNotFound, gin.H{
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Embeddings API is not supported for this platform",
					},
				})
				return
			}
			h.OpenAIGateway.Embeddings(c)
		})
		gateway.POST("/images/generations", handleImagesGenerations)
		gateway.POST("/images/edits", handleImagesEdits)
	}

	// Gemini 原生 API 兼容层（Gemini SDK/CLI 直连）
	gemini := r.Group("/v1beta")
	gemini.Use(bodyLimit)
	gemini.Use(clientRequestID)
	gemini.Use(opsErrorLogger)
	gemini.Use(endpointNorm)
	gemini.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	gemini.Use(requireGroupGoogle)
	{
		gemini.GET("/models", h.Gateway.GeminiV1BetaListModels)
		gemini.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		// Gin treats ":" as a param marker, but Gemini uses "{model}:{action}" in the same segment.
		gemini.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

	// OpenAI Responses API（不带v1前缀的别名）— auto-route based on group platform
	responsesHandler := func(c *gin.Context) {
		if isOpenAIResponsesCompatibleGatewayPlatform(c) {
			h.OpenAIGateway.Responses(c)
			return
		}
		h.Gateway.Responses(c)
	}
	r.POST("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, responsesHandler)
	r.POST("/responses/*subpath", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, responsesHandler)
	r.GET("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformGrok {
			rejectGrokUnsupportedEndpoint(c, "Responses WebSocket API")
			return
		}
		h.OpenAIGateway.ResponsesWebSocket(c)
	})
	r.GET("/realtime", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		h.OpenAIGateway.RealtimeWebSocket(c)
	})
	codexDirect := r.Group("/backend-api/codex")
	codexDirect.Use(bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic)
	{
		codexDirect.POST("/responses", responsesHandler)
		codexDirect.POST("/responses/*subpath", responsesHandler)
		codexDirect.GET("/responses", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformGrok {
				rejectGrokUnsupportedEndpoint(c, "Responses WebSocket API")
				return
			}
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
	}
	// OpenAI Chat Completions API（不带v1前缀的别名）— auto-route based on group platform
	r.POST("/chat/completions", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformGrok {
			rejectGrokUnsupportedEndpoint(c, "Chat Completions API")
			return
		}
		apiKey, _ := middleware.GetAPIKeyFromContext(c)
		if shouldUseOpenAIChatCompletionsHandler(apiKey, peekOpenAICompatibleRequestModel(c)) {
			h.OpenAIGateway.ChatCompletions(c)
			return
		}
		h.Gateway.ChatCompletions(c)
	})
	r.POST("/embeddings", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if !hasOpenAIGroup(c) {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Embeddings API is not supported for this platform",
				},
			})
			return
		}
		h.OpenAIGateway.Embeddings(c)
	})
	r.POST("/images/generations", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, handleImagesGenerations)
	r.POST("/images/edits", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, handleImagesEdits)

	// Antigravity 模型列表
	r.GET("/antigravity/models", gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, h.Gateway.AntigravityModels)

	// Antigravity 专用路由（仅使用 antigravity 账户，不混合调度）
	antigravityV1 := r.Group("/antigravity/v1")
	antigravityV1.Use(bodyLimit)
	antigravityV1.Use(clientRequestID)
	antigravityV1.Use(opsErrorLogger)
	antigravityV1.Use(endpointNorm)
	antigravityV1.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1.Use(gin.HandlerFunc(apiKeyAuth))
	antigravityV1.Use(requireGroupAnthropic)
	{
		antigravityV1.POST("/messages", h.Gateway.Messages)
		antigravityV1.POST("/messages/count_tokens", h.Gateway.CountTokens)
		antigravityV1.GET("/models", h.Gateway.AntigravityModels)
		antigravityV1.GET("/usage", h.Gateway.Usage)
	}

	antigravityV1Beta := r.Group("/antigravity/v1beta")
	antigravityV1Beta.Use(bodyLimit)
	antigravityV1Beta.Use(clientRequestID)
	antigravityV1Beta.Use(opsErrorLogger)
	antigravityV1Beta.Use(endpointNorm)
	antigravityV1Beta.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1Beta.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	antigravityV1Beta.Use(requireGroupGoogle)
	{
		antigravityV1Beta.GET("/models", h.Gateway.GeminiV1BetaListModels)
		antigravityV1Beta.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		antigravityV1Beta.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

}

// getGroupPlatform extracts the group platform from the API Key stored in context.
func getGroupPlatform(c *gin.Context) string {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		return ""
	}
	return apiKey.Group.Platform
}

func apiKeyHasGroupPlatform(apiKey *service.APIKey, platform string) bool {
	if apiKey == nil || platform == "" {
		return false
	}
	if apiKey.Group != nil && apiKey.Group.Platform == platform {
		return true
	}
	for i := range apiKey.Groups {
		if apiKey.Groups[i].Platform == platform {
			return true
		}
	}
	return false
}

func shouldUseOpenAIChatCompletionsHandler(apiKey *service.APIKey, model string) bool {
	if apiKeyHasPrimaryGroupPlatform(apiKey, service.PlatformOpenAI) {
		return true
	}
	return apiKeyHasGroupPlatform(apiKey, service.PlatformOpenAI) && isOpenAICompatibleChatModel(model)
}

func shouldUseOpenAIResponsesHandler(apiKey *service.APIKey, model string) bool {
	switch {
	case apiKeyHasPrimaryGroupPlatform(apiKey, service.PlatformOpenAI), apiKeyHasPrimaryGroupPlatform(apiKey, service.PlatformGrok):
		return true
	case apiKeyHasGroupPlatform(apiKey, service.PlatformOpenAI) && isOpenAICompatibleChatModel(model):
		return true
	case apiKeyHasGroupPlatform(apiKey, service.PlatformGrok) && isGrokCompatibleModel(model):
		return true
	default:
		return false
	}
}

func apiKeyHasPrimaryGroupPlatform(apiKey *service.APIKey, platform string) bool {
	return apiKey != nil && apiKey.Group != nil && apiKey.Group.Platform == platform
}

func isOpenAICompatibleChatModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gpt-") ||
		strings.HasPrefix(model, "o1") ||
		strings.HasPrefix(model, "o3") ||
		strings.HasPrefix(model, "o4")
}

func isGrokCompatibleModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "grok-")
}

func peekOpenAICompatibleRequestModel(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return ""
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if !gjson.ValidBytes(body) {
		return ""
	}
	return gjson.GetBytes(body, "model").String()
}
