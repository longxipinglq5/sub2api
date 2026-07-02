package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const BillingSessionCookieName = "sub2api_billing_session"

// NewJWTAuthMiddleware 创建 JWT 认证中间件
func NewJWTAuthMiddleware(authService *service.AuthService, userService *service.UserService) JWTAuthMiddleware {
	return JWTAuthMiddleware(jwtAuth(authService, userService, userService))
}

// BillingSessionAuthMiddleware accepts the normal application JWT from either
// Authorization or an HttpOnly cookie scoped to /api/billing.
func BillingSessionAuthMiddleware(jwtAuth JWTAuthMiddleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(c.GetHeader("Authorization")) == "" {
			if cookie, err := c.Cookie(BillingSessionCookieName); err == nil {
				token := strings.TrimSpace(cookie)
				if token != "" {
					c.Request.Header.Set("Authorization", "Bearer "+token)
				}
			}
		}
		gin.HandlerFunc(jwtAuth)(c)
	}
}

// BillingAccessAuthMiddleware accepts the product-facing billing upstream
// access key from Authorization/X-API-Key while keeping browser session
// compatibility for the local admin UI.
func BillingAccessAuthMiddleware(
	jwtAuth JWTAuthMiddleware,
	apiKeyService *service.APIKeyService,
	cfg *config.Config,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawAccessKey := billingAccessKeyFromHeaders(c)
		if rawAccessKey != "" {
			if authenticateBillingAccessKey(c, apiKeyService, cfg, rawAccessKey) {
				c.Next()
			}
			return
		}

		if strings.TrimSpace(c.GetHeader("Authorization")) == "" {
			if cookie, err := c.Cookie(BillingSessionCookieName); err == nil {
				token := strings.TrimSpace(cookie)
				if token != "" {
					c.Request.Header.Set("Authorization", "Bearer "+token)
				}
			}
		}
		gin.HandlerFunc(jwtAuth)(c)
	}
}

func billingAccessKeyFromHeaders(c *gin.Context) string {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && looksLikeSub2APIAccessKey(parts[1]) {
			return strings.TrimSpace(parts[1])
		}
	}
	if key := strings.TrimSpace(c.GetHeader("X-API-Key")); key != "" {
		return key
	}
	return strings.TrimSpace(c.GetHeader("X-Goog-API-Key"))
}

func looksLikeSub2APIAccessKey(raw string) bool {
	key := strings.TrimSpace(raw)
	return strings.HasPrefix(key, "sk-")
}

func authenticateBillingAccessKey(
	c *gin.Context,
	apiKeyService *service.APIKeyService,
	cfg *config.Config,
	rawAccessKey string,
) bool {
	if apiKeyService == nil {
		AbortWithError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Billing upstream access key validation is not configured")
		return false
	}

	apiKey, err := apiKeyService.GetByKey(c.Request.Context(), rawAccessKey)
	if err != nil {
		if errors.Is(err, service.ErrAPIKeyNotFound) {
			AbortWithError(c, http.StatusUnauthorized, "INVALID_API_KEY", "Invalid billing upstream access key")
			return false
		}
		AbortWithError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to validate billing upstream access key")
		return false
	}
	SetOpsFallbackAPIKey(c, apiKey)

	if !apiKey.IsActive() {
		AbortWithError(c, http.StatusUnauthorized, "API_KEY_DISABLED", "Billing upstream access key is disabled")
		return false
	}
	if !billingAPIKeyIPAllowed(c, apiKey, cfg) {
		return false
	}
	if apiKey.User == nil {
		AbortWithError(c, http.StatusUnauthorized, "USER_NOT_FOUND", "User associated with billing upstream access key not found")
		return false
	}
	if !apiKey.User.IsActive() {
		AbortWithError(c, http.StatusUnauthorized, "USER_INACTIVE", "User account is not active")
		return false
	}

	c.Set(string(ContextKeyAPIKey), apiKey)
	c.Set(string(ContextKeyUser), AuthSubject{
		UserID:      apiKey.User.ID,
		Concurrency: apiKey.User.Concurrency,
	})
	c.Set(string(ContextKeyUserRole), apiKey.User.Role)
	setGroupContext(c, apiKey.Group)
	_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
	return true
}

func billingAPIKeyIPAllowed(c *gin.Context, apiKey *service.APIKey, cfg *config.Config) bool {
	if len(apiKey.IPWhitelist) == 0 && len(apiKey.IPBlacklist) == 0 {
		return true
	}
	clientIP := ip.GetTrustedClientIP(c)
	if cfg != nil && cfg.TrustForwardedIPForAPIKeyACL() {
		clientIP = ip.GetClientIP(c)
	}
	allowed, _ := ip.CheckIPRestrictionWithCompiledRules(clientIP, apiKey.CompiledIPWhitelist, apiKey.CompiledIPBlacklist)
	if allowed {
		return true
	}
	if clientIP == "" {
		clientIP = "unknown"
	}
	AbortWithError(c, http.StatusForbidden, "ACCESS_DENIED", fmt.Sprintf("Access denied. Your IP is %s", clientIP))
	return false
}

func SetBillingSessionCookie(c *gin.Context, token string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     BillingSessionCookieName,
		Value:    token,
		Path:     "/api/billing",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   isHTTPSRequest(c),
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearBillingSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     BillingSessionCookieName,
		Value:    "",
		Path:     "/api/billing",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPSRequest(c),
		SameSite: http.SameSiteLaxMode,
	})
}

func isHTTPSRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if c.Request.TLS != nil {
		return true
	}
	proto := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")))
	return proto == "https"
}

type jwtUserReader interface {
	GetByID(ctx context.Context, id int64) (*service.User, error)
}

type userActivityToucher interface {
	TouchLastActiveForUser(ctx context.Context, user *service.User)
}

// jwtAuth JWT认证中间件实现
func jwtAuth(authService *service.AuthService, userService jwtUserReader, activityToucher userActivityToucher) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从Authorization header中提取token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			AbortWithError(c, 401, "UNAUTHORIZED", "Authorization header is required")
			return
		}

		// 验证Bearer scheme
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			AbortWithError(c, 401, "INVALID_AUTH_HEADER", "Authorization header format must be 'Bearer {token}'")
			return
		}

		tokenString := strings.TrimSpace(parts[1])
		if tokenString == "" {
			AbortWithError(c, 401, "EMPTY_TOKEN", "Token cannot be empty")
			return
		}

		// 验证token
		claims, err := authService.ValidateToken(tokenString)
		if err != nil {
			if errors.Is(err, service.ErrTokenExpired) {
				AbortWithError(c, 401, "TOKEN_EXPIRED", "Token has expired")
				return
			}
			AbortWithError(c, 401, "INVALID_TOKEN", "Invalid token")
			return
		}

		// 从数据库获取最新的用户信息
		user, err := userService.GetByID(c.Request.Context(), claims.UserID)
		if err != nil {
			AbortWithError(c, 401, "USER_NOT_FOUND", "User not found")
			return
		}

		// 检查用户状态
		if !user.IsActive() {
			AbortWithError(c, 401, "USER_INACTIVE", "User account is not active")
			return
		}

		// Security: Validate TokenVersion to ensure token hasn't been invalidated
		// This check ensures tokens issued before a password change are rejected
		if claims.TokenVersion != user.TokenVersion {
			AbortWithError(c, 401, "TOKEN_REVOKED", "Token has been revoked (password changed)")
			return
		}

		c.Set(string(ContextKeyUser), AuthSubject{
			UserID:      user.ID,
			Concurrency: user.Concurrency,
		})
		c.Set(string(ContextKeyUserRole), user.Role)
		if activityToucher != nil {
			activityToucher.TouchLastActiveForUser(c.Request.Context(), user)
		}

		c.Next()
	}
}

// Deprecated: prefer GetAuthSubjectFromContext in auth_subject.go.
