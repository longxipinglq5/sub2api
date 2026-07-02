package handler

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type BillingHandler struct {
	billing service.BillingAPI
}

func NewBillingHandler(billing service.BillingAPI) *BillingHandler {
	return &BillingHandler{billing: billing}
}

func (h *BillingHandler) Capabilities(c *gin.Context) {
	if _, ok := h.authSubject(c); !ok {
		return
	}
	data, err := h.billing.Capabilities(c.Request.Context())
	h.writeResult(c, data, err)
}

func (h *BillingHandler) ProviderKeys(c *gin.Context) {
	auth, ok := h.authSubject(c)
	if !ok {
		return
	}
	data, err := h.billing.ListProviderKeys(c.Request.Context(), auth, service.BillingProviderKeysQuery{
		Scope:            c.DefaultQuery("scope", "mine"),
		ProviderPlatform: c.Query("provider_platform"),
	})
	h.writeResult(c, data, err)
}

func (h *BillingHandler) Summary(c *gin.Context) {
	auth, ok := h.authSubject(c)
	if !ok {
		return
	}
	var req service.BillingQuery
	if err := h.bindBillingJSON(c, &req); err != nil {
		h.writeBillingError(c, newHandlerBillingError(http.StatusBadRequest, service.BillingErrInvalidArgument, "Invalid request: "+err.Error(), nil))
		return
	}
	data, err := h.billing.Summary(c.Request.Context(), auth, req)
	h.writeResult(c, data, err)
}

func (h *BillingHandler) Timeseries(c *gin.Context) {
	auth, ok := h.authSubject(c)
	if !ok {
		return
	}
	var req service.BillingQuery
	if err := h.bindBillingJSON(c, &req); err != nil {
		h.writeBillingError(c, newHandlerBillingError(http.StatusBadRequest, service.BillingErrInvalidArgument, "Invalid request: "+err.Error(), nil))
		return
	}
	data, err := h.billing.Timeseries(c.Request.Context(), auth, req)
	h.writeResult(c, data, err)
}

func (h *BillingHandler) Breakdown(c *gin.Context) {
	auth, ok := h.authSubject(c)
	if !ok {
		return
	}
	var req service.BillingBreakdownRequest
	if err := h.bindBillingJSON(c, &req); err != nil {
		h.writeBillingError(c, newHandlerBillingError(http.StatusBadRequest, service.BillingErrInvalidArgument, "Invalid request: "+err.Error(), nil))
		return
	}
	data, err := h.billing.Breakdown(c.Request.Context(), auth, req)
	h.writeResult(c, data, err)
}

func (h *BillingHandler) Events(c *gin.Context) {
	auth, ok := h.authSubject(c)
	if !ok {
		return
	}
	var req service.BillingQuery
	if err := h.bindBillingJSON(c, &req); err != nil {
		h.writeBillingError(c, newHandlerBillingError(http.StatusBadRequest, service.BillingErrInvalidArgument, "Invalid request: "+err.Error(), nil))
		return
	}
	data, err := h.billing.Events(c.Request.Context(), auth, req)
	h.writeResult(c, data, err)
}

func (h *BillingHandler) TriggerSync(c *gin.Context) {
	auth, ok := h.authSubject(c)
	if !ok {
		return
	}
	var req service.BillingSyncRequest
	if err := h.bindBillingJSON(c, &req); err != nil {
		h.writeBillingError(c, newHandlerBillingError(http.StatusBadRequest, service.BillingErrInvalidArgument, "Invalid request: "+err.Error(), nil))
		return
	}
	data, err := h.billing.TriggerSync(c.Request.Context(), auth, req)
	h.writeResult(c, data, err)
}

func (h *BillingHandler) GetSync(c *gin.Context) {
	auth, ok := h.authSubject(c)
	if !ok {
		return
	}
	data, err := h.billing.GetSync(c.Request.Context(), auth, c.Param("sync_id"))
	h.writeResult(c, data, err)
}

func (h *BillingHandler) CreateExport(c *gin.Context) {
	auth, ok := h.authSubject(c)
	if !ok {
		return
	}
	var req service.BillingExportRequest
	if err := h.bindBillingJSON(c, &req); err != nil {
		h.writeBillingError(c, newHandlerBillingError(http.StatusBadRequest, service.BillingErrInvalidArgument, "Invalid request: "+err.Error(), nil))
		return
	}
	data, err := h.billing.CreateExport(c.Request.Context(), auth, req)
	h.writeResult(c, data, err)
}

func (h *BillingHandler) DownloadExport(c *gin.Context) {
	auth, ok := h.authSubject(c)
	if !ok {
		return
	}
	data, err := h.billing.DownloadExport(c.Request.Context(), auth, c.Param("export_id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Content-Type", data.ContentType)
	c.Header("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(data.Filename, `"`, "")+`"`)
	c.Data(http.StatusOK, data.ContentType, data.Content)
}

func (h *BillingHandler) authSubject(c *gin.Context) (service.BillingAuthSubject, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		h.writeBillingError(c, newHandlerBillingError(http.StatusUnauthorized, service.BillingErrUnauthenticated, "User is not authenticated.", nil))
		return service.BillingAuthSubject{}, false
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	return service.BillingAuthSubject{UserID: subject.UserID, Role: role}, true
}

var forbiddenBillingSecretFields = map[string]struct{}{
	"api_key":          {},
	"raw_key":          {},
	"provider_api_key": {},
	"authorization":    {},
	"access_token":     {},
	"secret_key":       {},
	"upstream_token":   {},
}

func (h *BillingHandler) bindBillingJSON(c *gin.Context, out any) error {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	if field := findForbiddenBillingSecretField(raw, ""); field != "" {
		return errors.New("billing request contains forbidden raw provider key field " + field + "; use provider_key_id from /api/billing/provider-keys")
	}

	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return c.ShouldBindJSON(out)
}

func findForbiddenBillingSecretField(value any, path string) string {
	switch typed := value.(type) {
	case map[string]json.RawMessage:
		for field, raw := range typed {
			nextPath := field
			if path != "" {
				nextPath = path + "." + field
			}
			if _, forbidden := forbiddenBillingSecretFields[strings.ToLower(strings.TrimSpace(field))]; forbidden {
				return nextPath
			}
			var nested any
			if err := json.Unmarshal(raw, &nested); err == nil {
				if found := findForbiddenBillingSecretField(nested, nextPath); found != "" {
					return found
				}
			}
		}
	case map[string]any:
		for field, nested := range typed {
			nextPath := field
			if path != "" {
				nextPath = path + "." + field
			}
			if _, forbidden := forbiddenBillingSecretFields[strings.ToLower(strings.TrimSpace(field))]; forbidden {
				return nextPath
			}
			if found := findForbiddenBillingSecretField(nested, nextPath); found != "" {
				return found
			}
		}
	case []any:
		for _, nested := range typed {
			if found := findForbiddenBillingSecretField(nested, path); found != "" {
				return found
			}
		}
	}
	return ""
}

func (h *BillingHandler) writeResult(c *gin.Context, data any, err error) {
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":       0,
		"message":    "ok",
		"request_id": billingRequestID(c),
		"data":       data,
	})
}

func (h *BillingHandler) writeError(c *gin.Context, err error) {
	var apiErr *service.BillingAPIError
	if errors.As(err, &apiErr) {
		h.writeBillingError(c, apiErr)
		return
	}
	h.writeBillingError(c, newHandlerBillingError(http.StatusInternalServerError, service.BillingErrUnknown, "Unexpected billing error.", nil))
}

func (h *BillingHandler) writeBillingError(c *gin.Context, err *service.BillingAPIError) {
	status := err.HTTPStatus
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	body := gin.H{
		"code":       err.Code,
		"message":    err.Message,
		"request_id": billingRequestID(c),
	}
	if len(err.Details) > 0 {
		body["details"] = err.Details
	}
	c.JSON(status, body)
}

func newHandlerBillingError(status int, code, message string, details map[string]any) *service.BillingAPIError {
	return &service.BillingAPIError{HTTPStatus: status, Code: code, Message: message, Details: details}
}

func billingRequestID(c *gin.Context) string {
	id := strings.TrimSpace(c.GetHeader("X-Request-ID"))
	if id != "" {
		return id
	}
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_unknown"
	}
	return "req_" + hex.EncodeToString(b[:])
}
