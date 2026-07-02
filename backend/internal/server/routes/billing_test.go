package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type billingRouteUserRepo struct {
	service.UserRepository
	users map[int64]*service.User
}

type billingRouteAPIKeyRepo struct {
	service.APIKeyRepository
	apiKey *service.APIKey
}

func (r *billingRouteAPIKeyRepo) GetByKeyForAuth(_ context.Context, key string) (*service.APIKey, error) {
	if r.apiKey == nil || key != r.apiKey.Key {
		return nil, service.ErrAPIKeyNotFound
	}
	clone := *r.apiKey
	return &clone, nil
}

func (r *billingRouteAPIKeyRepo) UpdateLastUsed(_ context.Context, _ int64, _ time.Time) error {
	return nil
}

func (r *billingRouteUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	user, ok := r.users[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	return user, nil
}

func (r *billingRouteUserRepo) GetUserAvatar(_ context.Context, _ int64) (*service.UserAvatar, error) {
	return nil, nil
}

func (r *billingRouteUserRepo) UpdateUserLastActiveAt(_ context.Context, _ int64, _ time.Time) error {
	return nil
}

func TestBillingRoutesTranslateJWTErrorToBillingEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	RegisterBillingRoutes(
		router,
		&handler.Handlers{Billing: handler.NewBillingHandler(nil)},
		middleware.JWTAuthMiddleware(func(c *gin.Context) {
			middleware.AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization header is required")
		}),
		nil,
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/billing/capabilities", nil)
	req.Header.Set("X-Request-ID", "req-route-auth")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "UNAUTHENTICATED", body["code"])
	require.Equal(t, "Authorization header is required", body["message"])
	require.Equal(t, "req-route-auth", body["request_id"])
}

func TestBillingRoutesAcceptApplicationSessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:           7,
		Email:        "billing-user@example.com",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		TokenVersion: 1,
	}
	cfg := &config.Config{}
	cfg.JWT.Secret = "billing-route-cookie-secret"
	cfg.JWT.AccessTokenExpireMinutes = 60
	userRepo := &billingRouteUserRepo{users: map[int64]*service.User{user.ID: user}}
	authSvc := service.NewAuthService(nil, userRepo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	userSvc := service.NewUserService(userRepo, nil, nil, nil)
	token, err := authSvc.GenerateToken(user)
	require.NoError(t, err)

	router := gin.New()
	RegisterBillingRoutes(
		router,
		&handler.Handlers{Billing: handler.NewBillingHandler(&handlerBillingRouteStubService{})},
		middleware.NewJWTAuthMiddleware(authSvc, userSvc),
		nil,
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/billing/capabilities", nil)
	req.Header.Set("X-Request-ID", "req-cookie-auth")
	req.AddCookie(&http.Cookie{Name: middleware.BillingSessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, float64(0), body["code"])
	require.Equal(t, "req-cookie-auth", body["request_id"])
}

func TestBillingRoutesStillAcceptApplicationJWTBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:           8,
		Email:        "billing-jwt@example.com",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		TokenVersion: 1,
	}
	cfg := &config.Config{}
	cfg.JWT.Secret = "billing-route-jwt-secret"
	cfg.JWT.AccessTokenExpireMinutes = 60
	userRepo := &billingRouteUserRepo{users: map[int64]*service.User{user.ID: user}}
	authSvc := service.NewAuthService(nil, userRepo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	userSvc := service.NewUserService(userRepo, nil, nil, nil)
	token, err := authSvc.GenerateToken(user)
	require.NoError(t, err)

	apiKeySvc := service.NewAPIKeyService(&billingRouteAPIKeyRepo{}, nil, nil, nil, nil, nil, cfg)
	billingSvc := &handlerBillingRouteStubService{}
	router := gin.New()
	RegisterBillingRoutes(
		router,
		&handler.Handlers{Billing: handler.NewBillingHandler(billingSvc)},
		middleware.NewJWTAuthMiddleware(authSvc, userSvc),
		apiKeySvc,
		cfg,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/billing/provider-keys?scope=mine", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Request-ID", "req-jwt-bearer")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, user.ID, billingSvc.lastAuth.UserID)
}

func TestBillingRoutesAcceptAPIKeyAsUpstreamKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:           9,
		Email:        "billing-api-key@example.com",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		TokenVersion: 1,
		Balance:      0,
		Concurrency:  4,
	}
	apiKey := &service.APIKey{
		ID:     101,
		UserID: user.ID,
		Key:    "sk-billing-upstream-test",
		Status: service.StatusActive,
		User:   user,
	}
	cfg := &config.Config{}
	apiKeySvc := service.NewAPIKeyService(&billingRouteAPIKeyRepo{apiKey: apiKey}, nil, nil, nil, nil, nil, cfg)

	for name, configureHeader := range map[string]func(*http.Request){
		"authorization_bearer": func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+apiKey.Key)
		},
		"x_api_key": func(req *http.Request) {
			req.Header.Set("X-API-Key", apiKey.Key)
		},
	} {
		t.Run(name, func(t *testing.T) {
			billingSvc := &handlerBillingRouteStubService{}
			router := gin.New()
			RegisterBillingRoutes(
				router,
				&handler.Handlers{Billing: handler.NewBillingHandler(billingSvc)},
				middleware.NewJWTAuthMiddleware(nil, nil),
				apiKeySvc,
				cfg,
			)

			req := httptest.NewRequest(http.MethodGet, "/api/billing/provider-keys?scope=mine", nil)
			req.Header.Set("X-Request-ID", "req-billing-api-key")
			configureHeader(req)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)

			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, float64(0), body["code"])
			require.Equal(t, "req-billing-api-key", body["request_id"])
			require.Equal(t, user.ID, billingSvc.lastAuth.UserID)
			require.Equal(t, user.Role, billingSvc.lastAuth.Role)
		})
	}
}

func TestBillingRoutesRejectInvalidAPIKeyWithoutJWTFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	apiKeySvc := service.NewAPIKeyService(&billingRouteAPIKeyRepo{}, nil, nil, nil, nil, nil, cfg)
	jwtCalls := 0
	router := gin.New()
	RegisterBillingRoutes(
		router,
		&handler.Handlers{Billing: handler.NewBillingHandler(&handlerBillingRouteStubService{})},
		middleware.JWTAuthMiddleware(func(c *gin.Context) {
			jwtCalls++
			middleware.AbortWithError(c, http.StatusUnauthorized, "INVALID_TOKEN", "Invalid token")
		}),
		apiKeySvc,
		cfg,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/billing/capabilities", nil)
	req.Header.Set("Authorization", "Bearer sk-invalid-billing-upstream-key")
	req.Header.Set("X-Request-ID", "req-invalid-billing-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 0, jwtCalls)

	decoder := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	var body map[string]any
	require.NoError(t, decoder.Decode(&body))
	require.Equal(t, "UNAUTHENTICATED", body["code"])
	require.Equal(t, "Invalid billing upstream access key", body["message"])
	require.Equal(t, "req-invalid-billing-key", body["request_id"])

	var extra map[string]any
	require.ErrorIs(t, decoder.Decode(&extra), io.EOF)
}

type handlerBillingRouteStubService struct {
	lastAuth service.BillingAuthSubject
}

func (s *handlerBillingRouteStubService) Capabilities(context.Context) (service.BillingCapabilitiesResponse, error) {
	return service.BillingCapabilitiesResponse{Features: map[string]bool{"multi_key_query": false}}, nil
}

func (s *handlerBillingRouteStubService) ListProviderKeys(_ context.Context, auth service.BillingAuthSubject, _ service.BillingProviderKeysQuery) (service.BillingProviderKeysResponse, error) {
	s.lastAuth = auth
	return service.BillingProviderKeysResponse{}, nil
}

func (s *handlerBillingRouteStubService) Summary(context.Context, service.BillingAuthSubject, service.BillingQuery) (service.BillingSummaryResponse, error) {
	return service.BillingSummaryResponse{}, nil
}

func (s *handlerBillingRouteStubService) Timeseries(context.Context, service.BillingAuthSubject, service.BillingQuery) (service.BillingTimeseriesResponse, error) {
	return service.BillingTimeseriesResponse{}, nil
}

func (s *handlerBillingRouteStubService) Breakdown(context.Context, service.BillingAuthSubject, service.BillingBreakdownRequest) (service.BillingBreakdownResponse, error) {
	return service.BillingBreakdownResponse{}, nil
}

func (s *handlerBillingRouteStubService) Events(context.Context, service.BillingAuthSubject, service.BillingQuery) (service.BillingEventsResponse, error) {
	return service.BillingEventsResponse{}, nil
}

func (s *handlerBillingRouteStubService) TriggerSync(context.Context, service.BillingAuthSubject, service.BillingSyncRequest) (service.BillingSyncJob, error) {
	return service.BillingSyncJob{}, nil
}

func (s *handlerBillingRouteStubService) GetSync(context.Context, service.BillingAuthSubject, string) (service.BillingSyncJob, error) {
	return service.BillingSyncJob{}, nil
}

func (s *handlerBillingRouteStubService) CreateExport(context.Context, service.BillingAuthSubject, service.BillingExportRequest) (service.BillingExportResponse, error) {
	return service.BillingExportResponse{}, nil
}

func (s *handlerBillingRouteStubService) DownloadExport(context.Context, service.BillingAuthSubject, string) (service.BillingExportDownload, error) {
	return service.BillingExportDownload{}, nil
}
