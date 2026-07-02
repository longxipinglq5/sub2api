package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type authBillingCookieUserRepo struct {
	service.UserRepository
	users map[int64]*service.User
}

func (r *authBillingCookieUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	user, ok := r.users[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	return user, nil
}

func (r *authBillingCookieUserRepo) GetUserAvatar(_ context.Context, _ int64) (*service.UserAvatar, error) {
	return nil, nil
}

func (r *authBillingCookieUserRepo) UpdateUserLastActiveAt(_ context.Context, _ int64, _ time.Time) error {
	return nil
}

func TestAuthHandlerRespondWithTokenPairSetsBillingSessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:           7,
		Email:        "billing-user@example.com",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		TokenVersion: 1,
	}
	cfg := &config.Config{}
	cfg.JWT.Secret = "billing-cookie-secret"
	cfg.JWT.AccessTokenExpireMinutes = 60
	userRepo := &authBillingCookieUserRepo{users: map[int64]*service.User{user.ID: user}}
	authSvc := service.NewAuthService(nil, userRepo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	handler := &AuthHandler{authService: authSvc}

	router := gin.New()
	router.GET("/login-test", func(c *gin.Context) {
		handler.respondWithTokenPair(c, user)
	})

	req := httptest.NewRequest(http.MethodGet, "/login-test", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	cookie := findAuthBillingCookie(rec.Result().Cookies(), middleware.BillingSessionCookieName)
	require.NotNil(t, cookie)
	require.NotEmpty(t, cookie.Value)
	require.Equal(t, "/api/billing", cookie.Path)
	require.True(t, cookie.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
}

func findAuthBillingCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == name {
			return cookie
		}
	}
	return nil
}
