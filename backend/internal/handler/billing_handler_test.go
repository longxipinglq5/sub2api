package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type billingHandlerStubService struct {
	capabilitiesResp service.BillingCapabilitiesResponse
	accountResp      service.BillingAccountStatus
	listKeysResp     service.BillingProviderKeysResponse
	summaryResp      service.BillingSummaryResponse
	lastAuth         service.BillingAuthSubject
	accountCalled    bool
	summaryCalled    bool
}

func (s *billingHandlerStubService) Capabilities(context.Context) (service.BillingCapabilitiesResponse, error) {
	return s.capabilitiesResp, nil
}

func (s *billingHandlerStubService) ListProviderKeys(_ context.Context, auth service.BillingAuthSubject, _ service.BillingProviderKeysQuery) (service.BillingProviderKeysResponse, error) {
	s.lastAuth = auth
	return s.listKeysResp, nil
}

func (s *billingHandlerStubService) AccountStatus(_ context.Context, auth service.BillingAuthSubject) (service.BillingAccountStatus, error) {
	s.lastAuth = auth
	s.accountCalled = true
	return s.accountResp, nil
}

func (s *billingHandlerStubService) Summary(_ context.Context, auth service.BillingAuthSubject, _ service.BillingQuery) (service.BillingSummaryResponse, error) {
	s.lastAuth = auth
	s.summaryCalled = true
	return s.summaryResp, nil
}

func (s *billingHandlerStubService) Timeseries(context.Context, service.BillingAuthSubject, service.BillingQuery) (service.BillingTimeseriesResponse, error) {
	return service.BillingTimeseriesResponse{}, nil
}

func (s *billingHandlerStubService) Breakdown(context.Context, service.BillingAuthSubject, service.BillingBreakdownRequest) (service.BillingBreakdownResponse, error) {
	return service.BillingBreakdownResponse{}, nil
}

func (s *billingHandlerStubService) Events(context.Context, service.BillingAuthSubject, service.BillingQuery) (service.BillingEventsResponse, error) {
	return service.BillingEventsResponse{}, nil
}

func (s *billingHandlerStubService) TriggerSync(context.Context, service.BillingAuthSubject, service.BillingSyncRequest) (service.BillingSyncJob, error) {
	return service.BillingSyncJob{}, nil
}

func (s *billingHandlerStubService) GetSync(context.Context, service.BillingAuthSubject, string) (service.BillingSyncJob, error) {
	return service.BillingSyncJob{}, nil
}

func (s *billingHandlerStubService) CreateExport(context.Context, service.BillingAuthSubject, service.BillingExportRequest) (service.BillingExportResponse, error) {
	return service.BillingExportResponse{}, nil
}

func (s *billingHandlerStubService) DownloadExport(context.Context, service.BillingAuthSubject, string) (service.BillingExportDownload, error) {
	return service.BillingExportDownload{}, nil
}

func TestBillingHandlerCapabilitiesEnvelopeEchoesRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stub := &billingHandlerStubService{
		capabilitiesResp: service.BillingCapabilitiesResponse{
			SupportedPlatforms:     []string{service.PlatformOpenAI},
			SupportedGranularities: []string{"hour", "day", "month"},
			DefaultTimezone:        "Asia/Shanghai",
			DefaultCurrency:        "CNY",
			Features: map[string]bool{
				"events":          true,
				"multi_key_query": false,
			},
		},
	}
	handler := NewBillingHandler(stub)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
		c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleUser)
		c.Next()
	})
	router.GET("/api/billing/capabilities", handler.Capabilities)

	req := httptest.NewRequest(http.MethodGet, "/api/billing/capabilities", nil)
	req.Header.Set("X-Request-ID", "req-test-001")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, float64(0), body["code"])
	require.Equal(t, "ok", body["message"])
	require.Equal(t, "req-test-001", body["request_id"])
	require.NotNil(t, body["data"])
}

func TestBillingHandlerProviderKeysRequiresAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewBillingHandler(&billingHandlerStubService{})
	router := gin.New()
	router.GET("/api/billing/provider-keys", handler.ProviderKeys)

	req := httptest.NewRequest(http.MethodGet, "/api/billing/provider-keys", nil)
	req.Header.Set("X-Request-ID", "req-auth")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "UNAUTHENTICATED", body["code"])
	require.Equal(t, "req-auth", body["request_id"])
}

func TestBillingHandlerAccountStatusPassesAuthenticatedSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)

	periodStart := int64(1782748800000)
	periodEnd := int64(1785340800000)
	usedTokens := int64(1234)
	stub := &billingHandlerStubService{
		accountResp: service.BillingAccountStatus{
			Subscription: &service.BillingSubscription{
				PlanName:      "Professional",
				PeriodStartMS: &periodStart,
				PeriodEndMS:   &periodEnd,
				UsedTokens:    &usedTokens,
			},
			Balance: &service.BillingAccountBalance{
				Amount:   88.5,
				Currency: "USD",
			},
		},
	}
	handler := NewBillingHandler(stub)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
		c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleUser)
		c.Next()
	})
	router.GET("/api/billing/account", handler.AccountStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/billing/account", nil)
	req.Header.Set("X-Request-ID", "req-account")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, stub.accountCalled)
	require.Equal(t, int64(7), stub.lastAuth.UserID)
	require.Equal(t, service.RoleUser, stub.lastAuth.Role)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, float64(0), body["code"])
	require.Equal(t, "ok", body["message"])
	require.Equal(t, "req-account", body["request_id"])
	data := body["data"].(map[string]any)
	require.NotNil(t, data["subscription"])
	require.NotNil(t, data["balance"])
}

func TestBillingHandlerSummaryPassesAuthenticatedSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stub := &billingHandlerStubService{
		summaryResp: service.BillingSummaryResponse{
			Query: service.BillingQuery{
				ProviderKeyID: "pk_apikey_42",
				StartMS:       1782748800000,
				EndMS:         1782835200000,
				Timezone:      "Asia/Shanghai",
			},
			Summary:   service.BillingSummary{Currency: "CNY"},
			Freshness: service.DefaultBillingFreshness(),
		},
	}
	handler := NewBillingHandler(stub)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
		c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleAdmin)
		c.Next()
	})
	router.POST("/api/billing/summary", handler.Summary)

	req := httptest.NewRequest(http.MethodPost, "/api/billing/summary", strings.NewReader(`{
		"provider_key_id":"pk_apikey_42",
		"start_ms":1782748800000,
		"end_ms":1782835200000,
		"timezone":"Asia/Shanghai"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-summary")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(7), stub.lastAuth.UserID)
	require.Equal(t, service.RoleAdmin, stub.lastAuth.Role)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, float64(0), body["code"])
	require.Equal(t, "ok", body["message"])
	require.Equal(t, "req-summary", body["request_id"])
}

func TestBillingHandlerSummaryRejectsRawProviderKeyField(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stub := &billingHandlerStubService{}
	handler := NewBillingHandler(stub)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
		c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleAdmin)
		c.Next()
	})
	router.POST("/api/billing/summary", handler.Summary)

	req := httptest.NewRequest(http.MethodPost, "/api/billing/summary", strings.NewReader(`{
		"provider_key_id":"pk_apikey_42",
		"api_key":"sk-live-secret-ABCD",
		"start_ms":1782748800000,
		"end_ms":1782835200000
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, stub.summaryCalled)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, service.BillingErrInvalidArgument, body["code"])
	require.Equal(t, "req-secret", body["request_id"])
	require.NotContains(t, rec.Body.String(), "sk-live-secret-ABCD")
	require.Contains(t, rec.Body.String(), "api_key")
}

func TestBillingHandlerSummaryRejectsNestedRawProviderKeyField(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stub := &billingHandlerStubService{}
	handler := NewBillingHandler(stub)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
		c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleAdmin)
		c.Next()
	})
	router.POST("/api/billing/summary", handler.Summary)

	req := httptest.NewRequest(http.MethodPost, "/api/billing/summary", strings.NewReader(`{
		"provider_key_id":"pk_apikey_42",
		"credentials":{
			"api_key":"sk-live-secret-ABCD"
		},
		"start_ms":1782748800000,
		"end_ms":1782835200000
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-secret-nested")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, stub.summaryCalled)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, service.BillingErrInvalidArgument, body["code"])
	require.Equal(t, "req-secret-nested", body["request_id"])
	require.NotContains(t, rec.Body.String(), "sk-live-secret-ABCD")
	require.Contains(t, rec.Body.String(), "credentials.api_key")
}
