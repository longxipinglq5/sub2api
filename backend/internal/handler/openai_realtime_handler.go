package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RealtimeWebSocket handles the server-side OpenAI Realtime WebSocket API.
// The client connection is held to one upstream account for its full lifetime.
func (h *OpenAIGatewayHandler) RealtimeWebSocket(c *gin.Context) {
	if c == nil || c.Request == nil || !isOpenAIWSUpgradeRequest(c.Request) {
		if c != nil {
			h.errorResponse(c, http.StatusUpgradeRequired, "invalid_request_error", "WebSocket upgrade required (Upgrade: websocket)")
		}
		return
	}
	setOpenAIClientTransportWS(c)
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqModel := strings.TrimSpace(c.Query("model"))
	if reqModel == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model query parameter is required")
		return
	}
	setOpsRequestContext(c, reqModel, true)
	setOpsEndpointContext(c, "", int16(service.RequestTypeWSV2))
	reqLog := requestLogger(c, "handler.openai_gateway.realtime_ws",
		zap.Int64("user_id", subject.UserID), zap.Int64("api_key_id", apiKey.ID), zap.String("model", reqModel))
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}
	if _, err := resolveEffectiveAPIKeyGroupForPlatform(c.Request.Context(), h.gatewayService, apiKey, reqModel, service.PlatformOpenAI); err != nil {
		h.errorResponse(c, http.StatusNotFound, "invalid_request_error", err.Error())
		return
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		h.errorResponse(c, http.StatusPaymentRequired, "insufficient_quota", "billing check failed")
		return
	}

	wsConn, err := coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
	if err != nil {
		reqLog.Warn("openai.realtime_websocket_accept_failed", zap.Error(err))
		return
	}
	defer func() { _ = wsConn.CloseNow() }()
	wsConn.SetReadLimit(service.ResolveOpenAIWSClientReadLimitBytes(h.cfg))

	ctx := c.Request.Context()
	userRelease, acquired, err := h.concurrencyHelper.TryAcquireUserSlot(ctx, subject.UserID, subject.Concurrency)
	if err != nil || !acquired {
		closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "too many concurrent requests, please retry later")
		return
	}
	defer userRelease()

	mapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, reqModel)
	upstreamModel := strings.TrimSpace(mapping.MappedModel)
	if upstreamModel == "" {
		upstreamModel = reqModel
	}
	sessionHash := h.gatewayService.GenerateSessionHashWithFallback(c, []byte(fmt.Sprintf(`{"model":%q,"nonce":%q}`, reqModel, uuid.NewString())), uuid.NewString())
	failedIDs := make(map[int64]struct{})
	var lastFailover *service.UpstreamFailoverError
	for switchCount := 0; ; switchCount++ {
		selection, decision, selectErr := h.gatewayService.SelectAccountWithSchedulerForCapability(ctx, apiKey.GroupID, "", sessionHash, reqModel, failedIDs, service.OpenAIUpstreamTransportRealtimeWebsocket, service.OpenAIEndpointCapabilityRealtime, false, service.PlatformOpenAI)
		if selectErr != nil || selection == nil || selection.Account == nil {
			if lastFailover != nil {
				closeOpenAIWSFailoverExhausted(wsConn, lastFailover)
			} else {
				closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "no available realtime account")
			}
			return
		}
		account := selection.Account
		var releaseAccount func()
		if selection.Acquired {
			releaseAccount = selection.ReleaseFunc
		} else if selection.WaitPlan != nil {
			releaseAccount, acquired, err = h.concurrencyHelper.TryAcquireAccountSlot(ctx, account.ID, selection.WaitPlan.MaxConcurrency)
			if err != nil || !acquired {
				failedIDs[account.ID] = struct{}{}
				lastFailover = &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}
				continue
			}
		} else {
			failedIDs[account.ID] = struct{}{}
			lastFailover = &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}
			continue
		}
		if releaseAccount == nil {
			releaseAccount = func() {}
		}
		token, _, tokenErr := h.gatewayService.GetAccessToken(ctx, account)
		if tokenErr != nil {
			releaseAccount()
			failedIDs[account.ID] = struct{}{}
			lastFailover = &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}
			continue
		}
		reqLog.Debug("openai.realtime_account_selected", zap.Int64("account_id", account.ID), zap.String("schedule_layer", decision.Layer))
		turnHooks := &service.OpenAIRealtimeHooks{OnTurnComplete: func(turn service.OpenAIRealtimeTurnResult) {
			if strings.TrimSpace(turn.RequestID) == "" || (turn.Usage.InputTokens == 0 && turn.Usage.OutputTokens == 0 && turn.Usage.AudioInputTokens == 0 && turn.Usage.AudioOutputTokens == 0) {
				return
			}
			result := &service.OpenAIForwardResult{RequestID: turn.RequestID, Model: reqModel, UpstreamModel: upstreamModel, Stream: true, OpenAIWSMode: true, Duration: turn.Duration, FirstTokenMs: turn.FirstTokenMs, Usage: turn.Usage}
			inbound := GetInboundEndpoint(c)
			upstream := DeriveUpstreamEndpoint(inbound, c.Request.URL.Path, account.Platform)
			h.submitUsageRecordTask(ctx, func(taskCtx context.Context) {
				_ = h.gatewayService.RecordUsage(taskCtx, &service.OpenAIRecordUsageInput{Result: result, APIKey: apiKey, User: apiKey.User, Account: account, Subscription: subscription, InboundEndpoint: inbound, UpstreamEndpoint: upstream, UserAgent: c.GetHeader("User-Agent"), IPAddress: ip.GetClientIP(c), APIKeyService: h.apiKeyService, QuotaPlatform: service.QuotaPlatform(ctx, apiKey), ChannelUsageFields: service.ChannelUsageFields{ChannelID: mapping.ChannelID, OriginalModel: reqModel, ChannelMappedModel: upstreamModel, BillingModelSource: mapping.BillingModelSource, ModelMappingChain: mapping.BuildModelMappingChain(reqModel, upstreamModel)}})
			})
		}}
		proxyErr := h.gatewayService.ProxyRealtimeWebSocket(ctx, c, wsConn, account, token, reqModel, upstreamModel, turnHooks)
		releaseAccount()
		if proxyErr == nil {
			return
		}
		var failoverErr *service.UpstreamFailoverError
		if !errors.As(proxyErr, &failoverErr) || switchCount >= h.maxAccountSwitches {
			if errors.As(proxyErr, &failoverErr) {
				closeOpenAIWSFailoverExhausted(wsConn, failoverErr)
			} else {
				closeOpenAIClientWS(wsConn, coderws.StatusInternalError, "realtime upstream connection failed")
			}
			return
		}
		failedIDs[account.ID] = struct{}{}
		lastFailover = failoverErr
		h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
		h.gatewayService.RecordOpenAIAccountSwitch()
	}
}
