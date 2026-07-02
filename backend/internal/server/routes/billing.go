package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterBillingRoutes registers the product-facing billing API contract at /api/billing.
func RegisterBillingRoutes(
	r *gin.Engine,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	apiKeyService *service.APIKeyService,
	cfg *config.Config,
) {
	billing := r.Group("/api/billing")
	billing.Use(middleware.BillingAccessAuthMiddleware(jwtAuth, apiKeyService, cfg))
	{
		billing.GET("/capabilities", h.Billing.Capabilities)
		billing.GET("/provider-keys", h.Billing.ProviderKeys)
		billing.POST("/summary", h.Billing.Summary)
		billing.POST("/timeseries", h.Billing.Timeseries)
		billing.POST("/breakdown", h.Billing.Breakdown)
		billing.POST("/events", h.Billing.Events)
		billing.POST("/sync", h.Billing.TriggerSync)
		billing.GET("/sync/:sync_id", h.Billing.GetSync)
		billing.POST("/export", h.Billing.CreateExport)
		billing.GET("/export/:export_id", h.Billing.DownloadExport)
	}
}
