package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

const (
	BillingErrInvalidArgument        = "INVALID_ARGUMENT"
	BillingErrInvalidProviderKey     = "INVALID_PROVIDER_KEY"
	BillingErrProviderNotSupported   = "PROVIDER_NOT_SUPPORTED"
	BillingErrUnsupportedGranularity = "UNSUPPORTED_GRANULARITY"
	BillingErrUnsupportedDimension   = "UNSUPPORTED_DIMENSION"
	BillingErrEventsNotSupported     = "EVENTS_NOT_SUPPORTED"
	BillingErrTimeRangeTooLarge      = "TIME_RANGE_TOO_LARGE"
	BillingErrUnauthenticated        = "UNAUTHENTICATED"
	BillingErrPermissionDenied       = "PERMISSION_DENIED"
	BillingErrNotFound               = "NOT_FOUND"
	BillingErrSyncInProgress         = "SYNC_IN_PROGRESS"
	BillingErrUpstreamRateLimited    = "UPSTREAM_RATE_LIMITED"
	BillingErrUpstreamUnavailable    = "UPSTREAM_UNAVAILABLE"
	BillingErrExportNotReady         = "EXPORT_NOT_READY"
	BillingErrUnknown                = "UNKNOWN_ERROR"
)

const (
	billingProviderKeyPrefix = "pk_apikey_"
	billingDefaultTimezone   = "Asia/Shanghai"
	billingDefaultCurrency   = "CNY"
	billingDefaultFXUSDCNY   = 7.3
	billingDefaultLimit      = 50
	billingMaxLimit          = 500
)

// BillingAPI defines the upstream billing contract used by HTTP handlers.
type BillingAPI interface {
	Capabilities(ctx context.Context) (BillingCapabilitiesResponse, error)
	ListProviderKeys(ctx context.Context, auth BillingAuthSubject, query BillingProviderKeysQuery) (BillingProviderKeysResponse, error)
	Summary(ctx context.Context, auth BillingAuthSubject, query BillingQuery) (BillingSummaryResponse, error)
	Timeseries(ctx context.Context, auth BillingAuthSubject, query BillingQuery) (BillingTimeseriesResponse, error)
	Breakdown(ctx context.Context, auth BillingAuthSubject, req BillingBreakdownRequest) (BillingBreakdownResponse, error)
	Events(ctx context.Context, auth BillingAuthSubject, query BillingQuery) (BillingEventsResponse, error)
	TriggerSync(ctx context.Context, auth BillingAuthSubject, req BillingSyncRequest) (BillingSyncJob, error)
	GetSync(ctx context.Context, auth BillingAuthSubject, syncID string) (BillingSyncJob, error)
	CreateExport(ctx context.Context, auth BillingAuthSubject, req BillingExportRequest) (BillingExportResponse, error)
	DownloadExport(ctx context.Context, auth BillingAuthSubject, exportID string) (BillingExportDownload, error)
}

type BillingAPIError struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	HTTPStatus int            `json:"-"`
	Details    map[string]any `json:"details,omitempty"`
}

func (e *BillingAPIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func newBillingError(status int, code, message string, details map[string]any) *BillingAPIError {
	return &BillingAPIError{Code: code, Message: message, HTTPStatus: status, Details: details}
}

type BillingAuthSubject struct {
	UserID int64
	Role   string
}

func (a BillingAuthSubject) IsAdmin() bool {
	return a.Role == RoleAdmin
}

type BillingProviderKeysQuery struct {
	Scope            string `json:"scope"`
	ProviderPlatform string `json:"provider_platform"`
}

type BillingQuery struct {
	ProviderKeyID    string   `json:"provider_key_id,omitempty"`
	ProviderKeyIDs   []string `json:"provider_key_ids,omitempty"`
	ProviderPlatform string   `json:"provider_platform,omitempty"`
	StartMS          int64    `json:"start_ms"`
	EndMS            int64    `json:"end_ms"`
	Timezone         string   `json:"timezone,omitempty"`
	Granularity      string   `json:"granularity,omitempty"`
	Model            string   `json:"model,omitempty"`
	Models           []string `json:"models,omitempty"`
	UserID           string   `json:"user_id,omitempty"`
	UserIDs          []string `json:"user_ids,omitempty"`
	Cursor           string   `json:"cursor,omitempty"`
	Limit            int      `json:"limit,omitempty"`
}

type BillingBreakdownRequest struct {
	BillingQuery
	Dimension string `json:"dimension"`
}

type BillingSyncRequest struct {
	ProviderKeyID    string `json:"provider_key_id"`
	ProviderPlatform string `json:"provider_platform,omitempty"`
	StartMS          int64  `json:"start_ms"`
	EndMS            int64  `json:"end_ms"`
	Force            bool   `json:"force,omitempty"`
}

type BillingExportRequest struct {
	BillingQuery
	Format    string `json:"format"`
	Level     string `json:"level"`
	Dimension string `json:"dimension,omitempty"`
}

type BillingCapabilitiesResponse struct {
	SupportedPlatforms     []string                    `json:"supported_platforms"`
	SupportedGranularities []string                    `json:"supported_granularities"`
	DefaultTimezone        string                      `json:"default_timezone"`
	DefaultCurrency        string                      `json:"default_currency"`
	MaxRanges              map[string]map[string]int64 `json:"max_ranges"`
	Features               map[string]bool             `json:"features"`
}

type BillingProviderKeysResponse struct {
	Items []BillingProviderKey `json:"items"`
}

type BillingProviderKey struct {
	ProviderKeyID            string         `json:"provider_key_id"`
	ProviderPlatform         string         `json:"provider_platform"`
	DisplayName              string         `json:"display_name"`
	MaskedKey                string         `json:"masked_key,omitempty"`
	SourceProviderID         string         `json:"source_provider_id,omitempty"`
	SourceProviderName       string         `json:"source_provider_name,omitempty"`
	KeyAlias                 string         `json:"key_alias,omitempty"`
	KeyFingerprint           string         `json:"key_fingerprint,omitempty"`
	KeyLast4                 string         `json:"key_last4,omitempty"`
	OwnerUserID              string         `json:"owner_user_id,omitempty"`
	OwnerUserName            string         `json:"owner_user_name,omitempty"`
	WorkspaceID              string         `json:"workspace_id,omitempty"`
	Status                   string         `json:"status"`
	BillingSupported         bool           `json:"billing_supported"`
	RealtimeSupported        bool           `json:"realtime_supported"`
	SyncSupported            bool           `json:"sync_supported"`
	SupportedGranularities   []string       `json:"supported_granularities"`
	EarliestAvailableStartMS *int64         `json:"earliest_available_start_ms"`
	LatestAvailableEndMS     *int64         `json:"latest_available_end_ms"`
	LastSyncedAtMS           *int64         `json:"last_synced_at_ms"`
	Metadata                 map[string]any `json:"metadata,omitempty"`
}

type BillingSummary struct {
	RequestCount              int64    `json:"request_count"`
	InputTokens               int64    `json:"input_tokens"`
	OutputTokens              int64    `json:"output_tokens"`
	TotalTokens               int64    `json:"total_tokens"`
	CachedInputTokens         int64    `json:"cached_input_tokens"`
	ReasoningTokens           int64    `json:"reasoning_tokens"`
	CostUSD                   float64  `json:"cost_usd"`
	CostCNY                   float64  `json:"cost_cny"`
	Currency                  string   `json:"currency"`
	ExchangeRate              *float64 `json:"exchange_rate"`
	AverageCostUSDPer1MTokens *float64 `json:"average_cost_usd_per_1m_tokens"`
	AverageCostCNYPer1MTokens *float64 `json:"average_cost_cny_per_1m_tokens"`
}

type BillingFreshness struct {
	LastSyncedAtMS      *int64 `json:"last_synced_at_ms"`
	NextSyncAvailableMS *int64 `json:"next_sync_available_at_ms"`
	IsSyncing           bool   `json:"is_syncing"`
	DataDelaySeconds    *int64 `json:"data_delay_seconds"`
	Source              string `json:"source"`
}

func DefaultBillingFreshness() BillingFreshness {
	return BillingFreshness{
		IsSyncing: false,
		Source:    "upstream_gateway",
	}
}

type BillingSummaryResponse struct {
	Query     BillingQuery     `json:"query"`
	Summary   BillingSummary   `json:"summary"`
	Freshness BillingFreshness `json:"freshness"`
}

type BillingTimeseriesPoint struct {
	BucketStartMS int64  `json:"bucket_start_ms"`
	BucketEndMS   int64  `json:"bucket_end_ms"`
	Timezone      string `json:"timezone"`
	BillingSummary
}

type BillingTimeseriesResponse struct {
	Items     []BillingTimeseriesPoint `json:"items"`
	Freshness BillingFreshness         `json:"freshness"`
}

type BillingBreakdownRow struct {
	Dimension               string         `json:"dimension"`
	Key                     string         `json:"key"`
	Label                   string         `json:"label"`
	PercentageOfTotalCost   float64        `json:"percentage_of_total_cost"`
	PercentageOfTotalTokens float64        `json:"percentage_of_total_tokens"`
	Metadata                map[string]any `json:"metadata,omitempty"`
	BillingSummary
}

type BillingBreakdownResponse struct {
	Items      []BillingBreakdownRow `json:"items"`
	NextCursor *string               `json:"next_cursor"`
	HasMore    bool                  `json:"has_more"`
	Freshness  BillingFreshness      `json:"freshness"`
}

type BillingEvent struct {
	ID                string         `json:"id"`
	ProviderEventID   string         `json:"provider_event_id,omitempty"`
	ProviderKeyID     string         `json:"provider_key_id"`
	ProviderPlatform  string         `json:"provider_platform"`
	ProviderName      string         `json:"provider_name,omitempty"`
	Model             string         `json:"model"`
	UserID            string         `json:"user_id,omitempty"`
	UserName          string         `json:"user_name,omitempty"`
	RequestID         string         `json:"request_id,omitempty"`
	UpstreamProjectID string         `json:"upstream_project_id,omitempty"`
	InputTokens       int64          `json:"input_tokens"`
	OutputTokens      int64          `json:"output_tokens"`
	TotalTokens       int64          `json:"total_tokens"`
	CachedInputTokens int64          `json:"cached_input_tokens"`
	ReasoningTokens   int64          `json:"reasoning_tokens"`
	CostUSD           float64        `json:"cost_usd"`
	CostCNY           float64        `json:"cost_cny"`
	Currency          string         `json:"currency"`
	ExchangeRate      *float64       `json:"exchange_rate"`
	Status            string         `json:"status"`
	OccurredAtMS      int64          `json:"occurred_at_ms"`
	CreatedAtMS       int64          `json:"created_at_ms,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type BillingEventsResponse struct {
	Items      []BillingEvent   `json:"items"`
	NextCursor *string          `json:"next_cursor"`
	HasMore    bool             `json:"has_more"`
	Freshness  BillingFreshness `json:"freshness"`
}

type BillingSyncJob struct {
	SyncID           string              `json:"sync_id"`
	Status           string              `json:"status"`
	ProviderKeyID    string              `json:"provider_key_id,omitempty"`
	ProviderPlatform string              `json:"provider_platform,omitempty"`
	StartMS          int64               `json:"start_ms"`
	EndMS            int64               `json:"end_ms"`
	Progress         *BillingJobProgress `json:"progress"`
	StartedAtMS      *int64              `json:"started_at_ms"`
	FinishedAtMS     *int64              `json:"finished_at_ms"`
	ErrorCode        string              `json:"error_code,omitempty"`
	ErrorMessage     string              `json:"error_message,omitempty"`
}

type BillingJobProgress struct {
	Current int64 `json:"current"`
	Total   int64 `json:"total"`
}

type BillingExportResponse struct {
	ExportID    string `json:"export_id"`
	Status      string `json:"status"`
	DownloadURL string `json:"download_url"`
	ExpiresAtMS int64  `json:"expires_at_ms"`
}

type BillingExportDownload struct {
	Filename    string
	ContentType string
	Content     []byte
}

type BillingAPIService struct {
	db           *sql.DB
	exchangeRate float64
	now          func() time.Time
}

func NewBillingAPIService(db *sql.DB) *BillingAPIService {
	return &BillingAPIService{
		db:           db,
		exchangeRate: billingDefaultFXUSDCNY,
		now:          time.Now,
	}
}

func (s *BillingAPIService) Capabilities(_ context.Context) (BillingCapabilitiesResponse, error) {
	return BillingCapabilitiesResponse{
		SupportedPlatforms:     []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, "azure_openai", PlatformGrok, PlatformAntigravity},
		SupportedGranularities: []string{"hour", "day", "month"},
		DefaultTimezone:        billingDefaultTimezone,
		DefaultCurrency:        billingDefaultCurrency,
		MaxRanges: map[string]map[string]int64{
			"hour":  {"days": 31},
			"day":   {"days": 366},
			"month": {"months": 36},
		},
		Features: map[string]bool{
			"sync":            true,
			"events":          true,
			"export_csv":      true,
			"export_xlsx":     false,
			"multi_key_query": true,
		},
	}, nil
}

func (s *BillingAPIService) ListProviderKeys(ctx context.Context, auth BillingAuthSubject, query BillingProviderKeysQuery) (BillingProviderKeysResponse, error) {
	if auth.UserID <= 0 {
		return BillingProviderKeysResponse{}, newBillingError(401, BillingErrUnauthenticated, "User is not authenticated.", nil)
	}
	scope := strings.TrimSpace(query.Scope)
	if scope == "" {
		scope = "mine"
	}
	if scope != "mine" && scope != "all" {
		return BillingProviderKeysResponse{}, newBillingError(400, BillingErrInvalidArgument, "scope must be mine or all.", nil)
	}
	if scope == "all" && !auth.IsAdmin() {
		return BillingProviderKeysResponse{}, newBillingError(403, BillingErrPermissionDenied, "scope=all requires admin permission.", nil)
	}
	platform := strings.TrimSpace(query.ProviderPlatform)

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ak.id,
			ak.key,
			ak.name,
			ak.user_id,
			u.email,
			COALESCE(NULLIF(u.username, ''), u.email) AS owner_name,
			ak.status,
			ak.created_at,
			ak.last_used_at,
			ak.expires_at,
			COALESCE(NULLIF(MAX(COALESCE(NULLIF(g.platform, ''), a.platform)), ''), 'unknown') AS provider_platform,
			MIN(ul.created_at) AS earliest_available_at,
			MAX(ul.created_at) AS latest_available_at,
			MAX(ul.created_at) AS last_synced_at
		FROM api_keys ak
		JOIN users u ON u.id = ak.user_id
		LEFT JOIN usage_logs ul ON ul.api_key_id = ak.id
		LEFT JOIN accounts a ON a.id = ul.account_id
		LEFT JOIN groups g ON g.id = ul.group_id
		WHERE ak.deleted_at IS NULL
			AND ($3 = 'all' OR ak.user_id = $1)
			AND ($2 = '' OR COALESCE(NULLIF(g.platform, ''), a.platform) = $2)
		GROUP BY ak.id, ak.key, ak.name, ak.user_id, u.email, u.username, ak.status, ak.created_at, ak.last_used_at, ak.expires_at
		ORDER BY ak.id DESC
	`, auth.UserID, platform, scope)
	if err != nil {
		return BillingProviderKeysResponse{}, err
	}
	defer rows.Close()

	out := BillingProviderKeysResponse{Items: []BillingProviderKey{}}
	for rows.Next() {
		var (
			id         int64
			key        string
			name       string
			userID     int64
			ownerEmail string
			ownerName  string
			status     string
			createdAt  time.Time
			lastUsedAt sql.NullTime
			expiresAt  sql.NullTime
			provider   string
			earliest   sql.NullTime
			latest     sql.NullTime
			lastSynced sql.NullTime
		)
		if err := rows.Scan(&id, &key, &name, &userID, &ownerEmail, &ownerName, &status, &createdAt, &lastUsedAt, &expiresAt, &provider, &earliest, &latest, &lastSynced); err != nil {
			return BillingProviderKeysResponse{}, err
		}
		item := BillingProviderKey{
			ProviderKeyID:          formatBillingProviderKeyID(id),
			ProviderPlatform:       provider,
			DisplayName:            name,
			MaskedKey:              maskBillingProviderKey(key),
			SourceProviderID:       formatBillingSourceProviderID(id),
			SourceProviderName:     name,
			KeyAlias:               name,
			KeyFingerprint:         fingerprintKey(key),
			KeyLast4:               last4(key),
			OwnerUserID:            fmt.Sprintf("user_%d", userID),
			OwnerUserName:          ownerName,
			Status:                 billingKeyStatus(status, expiresAt),
			BillingSupported:       true,
			RealtimeSupported:      false,
			SyncSupported:          true,
			SupportedGranularities: []string{"hour", "day", "month"},
			Metadata: map[string]any{
				"source":        "api_key",
				"created_at_ms": createdAt.UnixMilli(),
			},
		}
		if earliest.Valid {
			v := earliest.Time.UnixMilli()
			item.EarliestAvailableStartMS = &v
		}
		if latest.Valid {
			v := latest.Time.UnixMilli()
			item.LatestAvailableEndMS = &v
		}
		if lastSynced.Valid {
			v := lastSynced.Time.UnixMilli()
			item.LastSyncedAtMS = &v
		}
		if lastUsedAt.Valid {
			item.Metadata["last_used_at_ms"] = lastUsedAt.Time.UnixMilli()
		}
		if expiresAt.Valid {
			item.Metadata["expires_at_ms"] = expiresAt.Time.UnixMilli()
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}

func (s *BillingAPIService) Summary(ctx context.Context, auth BillingAuthSubject, query BillingQuery) (BillingSummaryResponse, error) {
	normalized, keyIDs, err := s.normalizeQuery(ctx, auth, query, false)
	if err != nil {
		return BillingSummaryResponse{}, err
	}
	summary, freshness, err := s.aggregateSummary(ctx, normalized, keyIDs)
	if err != nil {
		return BillingSummaryResponse{}, err
	}
	return BillingSummaryResponse{Query: normalized, Summary: summary, Freshness: freshness}, nil
}

func (s *BillingAPIService) Timeseries(ctx context.Context, auth BillingAuthSubject, query BillingQuery) (BillingTimeseriesResponse, error) {
	normalized, keyIDs, err := s.normalizeQuery(ctx, auth, query, false)
	if err != nil {
		return BillingTimeseriesResponse{}, err
	}
	if normalized.Granularity == "" {
		normalized.Granularity = "day"
	}
	if err := validateBillingRange(normalized.StartMS, normalized.EndMS, normalized.Granularity); err != nil {
		return BillingTimeseriesResponse{}, err
	}
	loc, err := time.LoadLocation(normalized.Timezone)
	if err != nil {
		return BillingTimeseriesResponse{}, newBillingError(400, BillingErrInvalidArgument, "timezone is invalid.", map[string]any{"timezone": normalized.Timezone})
	}
	buckets := buildBillingBuckets(normalized.StartMS, normalized.EndMS, normalized.Granularity, loc)
	resultByBucket, freshness, err := s.queryTimeseries(ctx, normalized, keyIDs, loc)
	if err != nil {
		return BillingTimeseriesResponse{}, err
	}
	items := make([]BillingTimeseriesPoint, 0, len(buckets))
	for _, bucket := range buckets {
		summary := resultByBucket[bucket.start.UnixMilli()]
		if summary.Currency == "" {
			summary = s.zeroSummary()
		}
		items = append(items, BillingTimeseriesPoint{
			BucketStartMS:  bucket.start.UnixMilli(),
			BucketEndMS:    bucket.end.UnixMilli(),
			Timezone:       normalized.Timezone,
			BillingSummary: summary,
		})
	}
	return BillingTimeseriesResponse{Items: items, Freshness: freshness}, nil
}

func (s *BillingAPIService) Breakdown(ctx context.Context, auth BillingAuthSubject, req BillingBreakdownRequest) (BillingBreakdownResponse, error) {
	normalized, keyIDs, err := s.normalizeQuery(ctx, auth, req.BillingQuery, false)
	if err != nil {
		return BillingBreakdownResponse{}, err
	}
	dimension := strings.TrimSpace(req.Dimension)
	if dimension == "" {
		dimension = "model"
	}
	if !isSupportedBillingDimension(dimension) {
		return BillingBreakdownResponse{}, newBillingError(400, BillingErrUnsupportedDimension, "Requested breakdown dimension is not supported.", map[string]any{"dimension": dimension})
	}
	total, freshness, err := s.aggregateSummary(ctx, normalized, keyIDs)
	if err != nil {
		return BillingBreakdownResponse{}, err
	}
	limit := normalizeBillingLimit(req.Limit)
	offset := parseBillingCursor(req.Cursor)
	rows, err := s.queryBreakdownRows(ctx, normalized, keyIDs, dimension, limit+1, offset)
	if err != nil {
		return BillingBreakdownResponse{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	for i := range rows {
		rows[i].PercentageOfTotalCost = percentage(rows[i].CostUSD, total.CostUSD)
		rows[i].PercentageOfTotalTokens = percentage(float64(rows[i].TotalTokens), float64(total.TotalTokens))
	}
	var next *string
	if hasMore {
		cursor := fmt.Sprintf("offset:%d", offset+limit)
		next = &cursor
	}
	return BillingBreakdownResponse{Items: rows, NextCursor: next, HasMore: hasMore, Freshness: freshness}, nil
}

func (s *BillingAPIService) Events(ctx context.Context, auth BillingAuthSubject, query BillingQuery) (BillingEventsResponse, error) {
	normalized, keyIDs, err := s.normalizeQuery(ctx, auth, query, false)
	if err != nil {
		return BillingEventsResponse{}, err
	}
	limit := normalizeBillingLimit(normalized.Limit)
	offset := parseBillingCursor(normalized.Cursor)
	events, freshness, err := s.queryEvents(ctx, normalized, keyIDs, limit+1, offset)
	if err != nil {
		return BillingEventsResponse{}, err
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	var next *string
	if hasMore {
		cursor := fmt.Sprintf("offset:%d", offset+limit)
		next = &cursor
	}
	return BillingEventsResponse{Items: events, NextCursor: next, HasMore: hasMore, Freshness: freshness}, nil
}

func (s *BillingAPIService) TriggerSync(ctx context.Context, auth BillingAuthSubject, req BillingSyncRequest) (BillingSyncJob, error) {
	query := BillingQuery{ProviderKeyID: req.ProviderKeyID, ProviderPlatform: req.ProviderPlatform, StartMS: req.StartMS, EndMS: req.EndMS}
	normalized, _, err := s.normalizeQuery(ctx, auth, query, true)
	if err != nil {
		return BillingSyncJob{}, err
	}
	nowMS := s.now().UnixMilli()
	job := BillingSyncJob{
		SyncID:           encodeBillingJobID("sync", normalized),
		Status:           "succeeded",
		ProviderKeyID:    normalized.ProviderKeyID,
		ProviderPlatform: normalized.ProviderPlatform,
		StartMS:          normalized.StartMS,
		EndMS:            normalized.EndMS,
		Progress:         &BillingJobProgress{Current: 100, Total: 100},
		StartedAtMS:      &nowMS,
		FinishedAtMS:     &nowMS,
	}
	return job, nil
}

func (s *BillingAPIService) GetSync(ctx context.Context, auth BillingAuthSubject, syncID string) (BillingSyncJob, error) {
	query, err := decodeBillingJobID("sync", syncID)
	if err != nil {
		return BillingSyncJob{}, newBillingError(404, BillingErrNotFound, "Sync job was not found.", map[string]any{"sync_id": syncID})
	}
	normalized, _, err := s.normalizeQuery(ctx, auth, query, true)
	if err != nil {
		return BillingSyncJob{}, err
	}
	nowMS := s.now().UnixMilli()
	return BillingSyncJob{
		SyncID:           syncID,
		Status:           "succeeded",
		ProviderKeyID:    normalized.ProviderKeyID,
		ProviderPlatform: normalized.ProviderPlatform,
		StartMS:          normalized.StartMS,
		EndMS:            normalized.EndMS,
		Progress:         &BillingJobProgress{Current: 100, Total: 100},
		StartedAtMS:      &nowMS,
		FinishedAtMS:     &nowMS,
	}, nil
}

func (s *BillingAPIService) CreateExport(ctx context.Context, auth BillingAuthSubject, req BillingExportRequest) (BillingExportResponse, error) {
	if req.Format == "" {
		req.Format = "csv"
	}
	if req.Level == "" {
		req.Level = "events"
	}
	if req.Format != "csv" {
		return BillingExportResponse{}, newBillingError(400, BillingErrInvalidArgument, "Only csv export is currently supported.", map[string]any{"format": req.Format})
	}
	if req.Level != "events" && req.Level != "summary" && req.Level != "timeseries" && req.Level != "breakdown" {
		return BillingExportResponse{}, newBillingError(400, BillingErrInvalidArgument, "export level is invalid.", map[string]any{"level": req.Level})
	}
	normalized, _, err := s.normalizeQuery(ctx, auth, req.BillingQuery, false)
	if err != nil {
		return BillingExportResponse{}, err
	}
	payload := billingExportPayload{Query: normalized, Format: req.Format, Level: req.Level, Dimension: req.Dimension}
	exportID := encodeBillingExportID(payload)
	return BillingExportResponse{
		ExportID:    exportID,
		Status:      "ready",
		DownloadURL: "/api/billing/export/" + exportID,
		ExpiresAtMS: s.now().Add(24 * time.Hour).UnixMilli(),
	}, nil
}

func (s *BillingAPIService) DownloadExport(ctx context.Context, auth BillingAuthSubject, exportID string) (BillingExportDownload, error) {
	payload, err := decodeBillingExportID(exportID)
	if err != nil {
		return BillingExportDownload{}, newBillingError(404, BillingErrNotFound, "Export was not found.", map[string]any{"export_id": exportID})
	}
	payload.Query.Limit = billingMaxLimit
	eventsResp, err := s.Events(ctx, auth, payload.Query)
	if err != nil {
		return BillingExportDownload{}, err
	}
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	_ = writer.Write([]string{"id", "provider_key_id", "provider_platform", "model", "request_id", "input_tokens", "output_tokens", "total_tokens", "cost_usd", "cost_cny", "status", "occurred_at_ms"})
	for _, item := range eventsResp.Items {
		_ = writer.Write([]string{
			item.ID,
			item.ProviderKeyID,
			item.ProviderPlatform,
			item.Model,
			item.RequestID,
			strconv.FormatInt(item.InputTokens, 10),
			strconv.FormatInt(item.OutputTokens, 10),
			strconv.FormatInt(item.TotalTokens, 10),
			strconv.FormatFloat(item.CostUSD, 'f', -1, 64),
			strconv.FormatFloat(item.CostCNY, 'f', -1, 64),
			item.Status,
			strconv.FormatInt(item.OccurredAtMS, 10),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return BillingExportDownload{}, err
	}
	return BillingExportDownload{
		Filename:    "centaurai-billing-" + time.UnixMilli(payload.Query.StartMS).Format("2006-01-02") + ".csv",
		ContentType: "text/csv; charset=utf-8",
		Content:     buf.Bytes(),
	}, nil
}

type billingKeyRecord struct {
	id        int64
	userID    int64
	status    string
	expiresAt sql.NullTime
}

func (s *BillingAPIService) normalizeQuery(ctx context.Context, auth BillingAuthSubject, query BillingQuery, requireSingleKey bool) (BillingQuery, []int64, error) {
	if auth.UserID <= 0 {
		return query, nil, newBillingError(401, BillingErrUnauthenticated, "User is not authenticated.", nil)
	}
	if query.UserID != "" || len(query.UserIDs) > 0 {
		if !auth.IsAdmin() {
			return query, nil, newBillingError(403, BillingErrPermissionDenied, "user_id and user_ids require admin permission.", nil)
		}
		query.UserID = strings.TrimSpace(query.UserID)
		if query.UserID != "" {
			if _, err := parseBillingUserID(query.UserID); err != nil {
				return query, nil, err
			}
		}
		trimmedUserIDs := make([]string, 0, len(query.UserIDs))
		for _, raw := range query.UserIDs {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if _, err := parseBillingUserID(raw); err != nil {
				return query, nil, err
			}
			trimmedUserIDs = append(trimmedUserIDs, raw)
		}
		query.UserIDs = trimmedUserIDs
	}
	if query.StartMS <= 0 || query.EndMS <= 0 || query.EndMS <= query.StartMS {
		return query, nil, newBillingError(400, BillingErrInvalidArgument, "start_ms and end_ms are required and must form a valid half-open range.", nil)
	}
	if query.Timezone == "" {
		query.Timezone = billingDefaultTimezone
	}
	if _, err := time.LoadLocation(query.Timezone); err != nil {
		return query, nil, newBillingError(400, BillingErrInvalidArgument, "timezone is invalid.", map[string]any{"timezone": query.Timezone})
	}
	if query.Granularity != "" {
		if err := validateBillingRange(query.StartMS, query.EndMS, query.Granularity); err != nil {
			return query, nil, err
		}
	}

	keyIDs, err := s.resolveBillingProviderKeyIDs(ctx, auth, query, requireSingleKey)
	if err != nil {
		return query, nil, err
	}
	query.ProviderPlatform = strings.TrimSpace(query.ProviderPlatform)
	query.Model = strings.TrimSpace(query.Model)
	query.Limit = normalizeBillingLimit(query.Limit)
	return query, keyIDs, nil
}

func (s *BillingAPIService) resolveBillingProviderKeyIDs(ctx context.Context, auth BillingAuthSubject, query BillingQuery, requireSingleKey bool) ([]int64, error) {
	var ids []int64
	providerKeyID := strings.TrimSpace(query.ProviderKeyID)
	hasProviderKeyID := providerKeyID != ""
	trimmedProviderKeyIDs := make([]string, 0, len(query.ProviderKeyIDs))
	for _, raw := range query.ProviderKeyIDs {
		raw = strings.TrimSpace(raw)
		if raw != "" {
			trimmedProviderKeyIDs = append(trimmedProviderKeyIDs, raw)
		}
	}

	if hasProviderKeyID && len(trimmedProviderKeyIDs) > 0 {
		return nil, newBillingError(400, BillingErrInvalidArgument, "provider_key_id and provider_key_ids are mutually exclusive.", nil)
	}
	if hasProviderKeyID {
		id, err := parseBillingProviderKeyID(providerKeyID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	} else if len(trimmedProviderKeyIDs) > 0 {
		for _, raw := range trimmedProviderKeyIDs {
			id, err := parseBillingProviderKeyID(raw)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
	} else {
		visibleIDs, err := s.listVisibleBillingKeyIDs(ctx, auth)
		if err != nil {
			return nil, err
		}
		ids = visibleIDs
	}
	if requireSingleKey && len(ids) != 1 {
		return nil, newBillingError(400, BillingErrInvalidArgument, "provider_key_id is required.", nil)
	}
	records, err := s.loadBillingKeyRecords(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(records) != len(uniqueInt64(ids)) {
		return nil, newBillingError(400, BillingErrInvalidProviderKey, "Provider key is invalid or not found.", nil)
	}
	now := s.now()
	for _, rec := range records {
		if !auth.IsAdmin() && rec.userID != auth.UserID {
			return nil, newBillingError(403, BillingErrPermissionDenied, "User cannot access this provider key.", map[string]any{"provider_key_id": formatBillingProviderKeyID(rec.id)})
		}
		if billingKeyStatus(rec.status, rec.expiresAt) != "active" {
			return nil, newBillingError(400, BillingErrInvalidProviderKey, "Provider key is disabled, expired, or invalid.", map[string]any{"provider_key_id": formatBillingProviderKeyID(rec.id)})
		}
		if rec.expiresAt.Valid && rec.expiresAt.Time.Before(now) {
			return nil, newBillingError(400, BillingErrInvalidProviderKey, "Provider key is expired.", map[string]any{"provider_key_id": formatBillingProviderKeyID(rec.id)})
		}
	}
	return uniqueInt64(ids), nil
}

func (s *BillingAPIService) loadBillingKeyRecords(ctx context.Context, ids []int64) (map[int64]billingKeyRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, status, expires_at FROM api_keys WHERE id = ANY($1) AND deleted_at IS NULL`, pq.Array(uniqueInt64(ids)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]billingKeyRecord)
	for rows.Next() {
		var rec billingKeyRecord
		if err := rows.Scan(&rec.id, &rec.userID, &rec.status, &rec.expiresAt); err != nil {
			return nil, err
		}
		out[rec.id] = rec
	}
	return out, rows.Err()
}

func (s *BillingAPIService) listVisibleBillingKeyIDs(ctx context.Context, auth BillingAuthSubject) ([]int64, error) {
	query := `SELECT id FROM api_keys WHERE deleted_at IS NULL AND status = $1`
	args := []any{StatusActive}
	if !auth.IsAdmin() {
		query += ` AND user_id = $2`
		args = append(args, auth.UserID)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *BillingAPIService) aggregateSummary(ctx context.Context, query BillingQuery, keyIDs []int64) (BillingSummary, BillingFreshness, error) {
	if len(keyIDs) == 0 {
		return s.zeroSummary(), DefaultBillingFreshness(), nil
	}
	start := time.UnixMilli(query.StartMS)
	end := time.UnixMilli(query.EndMS)
	sqlText := `
		SELECT
			COUNT(*) AS request_count,
			COALESCE(SUM(ul.input_tokens), 0) AS input_tokens,
			COALESCE(SUM(ul.output_tokens), 0) AS output_tokens,
			COALESCE(SUM(ul.cache_creation_tokens + ul.cache_read_tokens + ul.cache_creation_5m_tokens + ul.cache_creation_1h_tokens), 0) AS cached_input_tokens,
			0 AS reasoning_tokens,
			COALESCE(SUM(ul.actual_cost), 0) AS cost_usd,
			MAX(ul.created_at) AS last_synced_at
		FROM usage_logs ul
		LEFT JOIN accounts a ON a.id = ul.account_id
		LEFT JOIN groups g ON g.id = ul.group_id
		WHERE ul.created_at >= $1 AND ul.created_at < $2 AND ul.api_key_id = ANY($3)`
	args := []any{start, end, pq.Array(keyIDs)}
	sqlText, args = appendBillingUsageFilters(sqlText, args, query)

	var (
		requests   int64
		input      int64
		output     int64
		cached     int64
		reasoning  int64
		costUSD    float64
		lastSynced sql.NullTime
	)
	if err := s.db.QueryRowContext(ctx, sqlText, args...).Scan(&requests, &input, &output, &cached, &reasoning, &costUSD, &lastSynced); err != nil {
		return BillingSummary{}, BillingFreshness{}, err
	}
	summary := s.summaryFromParts(requests, input, output, cached, reasoning, costUSD)
	freshness := s.freshnessFromLastSynced(lastSynced)
	return summary, freshness, nil
}

func (s *BillingAPIService) queryTimeseries(ctx context.Context, query BillingQuery, keyIDs []int64, loc *time.Location) (map[int64]BillingSummary, BillingFreshness, error) {
	if len(keyIDs) == 0 {
		return map[int64]BillingSummary{}, DefaultBillingFreshness(), nil
	}
	start := time.UnixMilli(query.StartMS)
	end := time.UnixMilli(query.EndMS)
	bucketExpr := "date_trunc('day', ul.created_at AT TIME ZONE $4) AT TIME ZONE $4"
	switch query.Granularity {
	case "hour":
		bucketExpr = "date_trunc('hour', ul.created_at AT TIME ZONE $4) AT TIME ZONE $4"
	case "month":
		bucketExpr = "date_trunc('month', ul.created_at AT TIME ZONE $4) AT TIME ZONE $4"
	}
	sqlText := fmt.Sprintf(`
		SELECT
			%s AS bucket_start,
			COUNT(*) AS request_count,
			COALESCE(SUM(ul.input_tokens), 0) AS input_tokens,
			COALESCE(SUM(ul.output_tokens), 0) AS output_tokens,
			COALESCE(SUM(ul.cache_creation_tokens + ul.cache_read_tokens + ul.cache_creation_5m_tokens + ul.cache_creation_1h_tokens), 0) AS cached_input_tokens,
			0 AS reasoning_tokens,
			COALESCE(SUM(ul.actual_cost), 0) AS cost_usd,
			MAX(ul.created_at) AS last_synced_at
		FROM usage_logs ul
		LEFT JOIN accounts a ON a.id = ul.account_id
		LEFT JOIN groups g ON g.id = ul.group_id
		WHERE ul.created_at >= $1 AND ul.created_at < $2 AND ul.api_key_id = ANY($3)`, bucketExpr)
	args := []any{start, end, pq.Array(keyIDs), query.Timezone}
	sqlText, args = appendBillingUsageFilters(sqlText, args, query)
	sqlText += " GROUP BY bucket_start ORDER BY bucket_start ASC"

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, BillingFreshness{}, err
	}
	defer rows.Close()
	out := make(map[int64]BillingSummary)
	var newest sql.NullTime
	for rows.Next() {
		var (
			bucket     time.Time
			requests   int64
			input      int64
			output     int64
			cached     int64
			reasoning  int64
			costUSD    float64
			lastSynced sql.NullTime
		)
		if err := rows.Scan(&bucket, &requests, &input, &output, &cached, &reasoning, &costUSD, &lastSynced); err != nil {
			return nil, BillingFreshness{}, err
		}
		out[bucket.In(loc).UnixMilli()] = s.summaryFromParts(requests, input, output, cached, reasoning, costUSD)
		if lastSynced.Valid && (!newest.Valid || lastSynced.Time.After(newest.Time)) {
			newest = lastSynced
		}
	}
	if err := rows.Err(); err != nil {
		return nil, BillingFreshness{}, err
	}
	return out, s.freshnessFromLastSynced(newest), nil
}

func (s *BillingAPIService) queryBreakdownRows(ctx context.Context, query BillingQuery, keyIDs []int64, dimension string, limit, offset int) ([]BillingBreakdownRow, error) {
	if len(keyIDs) == 0 {
		return []BillingBreakdownRow{}, nil
	}
	dimKey, dimLabel := billingDimensionExpressions(dimension)
	sqlText := fmt.Sprintf(`
		SELECT
			%s AS dimension_key,
			%s AS dimension_label,
			COUNT(*) AS request_count,
			COALESCE(SUM(ul.input_tokens), 0) AS input_tokens,
			COALESCE(SUM(ul.output_tokens), 0) AS output_tokens,
			COALESCE(SUM(ul.cache_creation_tokens + ul.cache_read_tokens + ul.cache_creation_5m_tokens + ul.cache_creation_1h_tokens), 0) AS cached_input_tokens,
			0 AS reasoning_tokens,
			COALESCE(SUM(ul.actual_cost), 0) AS cost_usd
		FROM usage_logs ul
		LEFT JOIN accounts a ON a.id = ul.account_id
		LEFT JOIN groups g ON g.id = ul.group_id
		LEFT JOIN users u ON u.id = ul.user_id
		WHERE ul.created_at >= $1 AND ul.created_at < $2 AND ul.api_key_id = ANY($3)`, dimKey, dimLabel)
	args := []any{time.UnixMilli(query.StartMS), time.UnixMilli(query.EndMS), pq.Array(keyIDs)}
	sqlText, args = appendBillingUsageFilters(sqlText, args, query)
	sqlText += fmt.Sprintf(" GROUP BY dimension_key, dimension_label ORDER BY cost_usd DESC, request_count DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BillingBreakdownRow
	for rows.Next() {
		var (
			key       string
			label     string
			requests  int64
			input     int64
			output    int64
			cached    int64
			reasoning int64
			costUSD   float64
		)
		if err := rows.Scan(&key, &label, &requests, &input, &output, &cached, &reasoning, &costUSD); err != nil {
			return nil, err
		}
		if dimension == "provider_key" && key != "" {
			if id, err := strconv.ParseInt(key, 10, 64); err == nil {
				key = formatBillingProviderKeyID(id)
				label = key
			}
		}
		row := BillingBreakdownRow{
			Dimension:      dimension,
			Key:            key,
			Label:          label,
			BillingSummary: s.summaryFromParts(requests, input, output, cached, reasoning, costUSD),
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *BillingAPIService) queryEvents(ctx context.Context, query BillingQuery, keyIDs []int64, limit, offset int) ([]BillingEvent, BillingFreshness, error) {
	if len(keyIDs) == 0 {
		return []BillingEvent{}, DefaultBillingFreshness(), nil
	}
	sqlText := `
		SELECT
			ul.id,
			ul.request_id,
			ul.api_key_id,
			COALESCE(NULLIF(COALESCE(NULLIF(g.platform, ''), a.platform), ''), 'unknown') AS provider_platform,
			COALESCE(NULLIF(a.name, ''), COALESCE(NULLIF(COALESCE(NULLIF(g.platform, ''), a.platform), ''), 'unknown')) AS provider_name,
			COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) AS model,
			ul.user_id,
			COALESCE(NULLIF(u.username, ''), u.email, '') AS user_name,
			ul.input_tokens,
			ul.output_tokens,
			(ul.cache_creation_tokens + ul.cache_read_tokens + ul.cache_creation_5m_tokens + ul.cache_creation_1h_tokens) AS cached_input_tokens,
			0 AS reasoning_tokens,
			ul.actual_cost,
			ul.created_at
		FROM usage_logs ul
		LEFT JOIN accounts a ON a.id = ul.account_id
		LEFT JOIN groups g ON g.id = ul.group_id
		LEFT JOIN users u ON u.id = ul.user_id
		WHERE ul.created_at >= $1 AND ul.created_at < $2 AND ul.api_key_id = ANY($3)`
	args := []any{time.UnixMilli(query.StartMS), time.UnixMilli(query.EndMS), pq.Array(keyIDs)}
	sqlText, args = appendBillingUsageFilters(sqlText, args, query)
	sqlText += fmt.Sprintf(" ORDER BY ul.created_at DESC, ul.id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, BillingFreshness{}, err
	}
	defer rows.Close()
	var (
		out    []BillingEvent
		newest sql.NullTime
	)
	for rows.Next() {
		var (
			id        int64
			requestID string
			apiKeyID  int64
			platform  string
			provider  string
			model     string
			userID    int64
			userName  string
			input     int64
			output    int64
			cached    int64
			reasoning int64
			costUSD   float64
			createdAt time.Time
		)
		if err := rows.Scan(&id, &requestID, &apiKeyID, &platform, &provider, &model, &userID, &userName, &input, &output, &cached, &reasoning, &costUSD, &createdAt); err != nil {
			return nil, BillingFreshness{}, err
		}
		status := "completed"
		if costUSD <= 0 && input+output+cached+reasoning == 0 {
			status = "failed"
		}
		exchange := s.exchangeRate
		out = append(out, BillingEvent{
			ID:                fmt.Sprintf("evt_%d", id),
			ProviderEventID:   fmt.Sprintf("usage_log_%d", id),
			ProviderKeyID:     formatBillingProviderKeyID(apiKeyID),
			ProviderPlatform:  platform,
			ProviderName:      provider,
			Model:             model,
			UserID:            fmt.Sprintf("user_%d", userID),
			UserName:          userName,
			RequestID:         requestID,
			InputTokens:       input,
			OutputTokens:      output,
			TotalTokens:       input + output + cached + reasoning,
			CachedInputTokens: cached,
			ReasoningTokens:   reasoning,
			CostUSD:           roundBilling(costUSD),
			CostCNY:           roundBilling(costUSD * s.exchangeRate),
			Currency:          billingDefaultCurrency,
			ExchangeRate:      &exchange,
			Status:            status,
			OccurredAtMS:      createdAt.UnixMilli(),
			CreatedAtMS:       createdAt.UnixMilli(),
			Metadata:          map[string]any{"source": "upstream_gateway"},
		})
		if !newest.Valid || createdAt.After(newest.Time) {
			newest = sql.NullTime{Time: createdAt, Valid: true}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, BillingFreshness{}, err
	}
	return out, s.freshnessFromLastSynced(newest), nil
}

func appendBillingUsageFilters(sqlText string, args []any, query BillingQuery) (string, []any) {
	if query.ProviderPlatform != "" {
		sqlText += fmt.Sprintf(" AND COALESCE(NULLIF(g.platform, ''), a.platform) = $%d", len(args)+1)
		args = append(args, query.ProviderPlatform)
	}
	if query.Model != "" {
		sqlText += fmt.Sprintf(" AND COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) = $%d", len(args)+1)
		args = append(args, query.Model)
	}
	if len(query.Models) > 0 {
		sqlText += fmt.Sprintf(" AND COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) = ANY($%d)", len(args)+1)
		args = append(args, pq.Array(query.Models))
	}
	if query.UserID != "" {
		userID, err := parseBillingUserID(query.UserID)
		if err == nil {
			sqlText += fmt.Sprintf(" AND ul.user_id = $%d", len(args)+1)
			args = append(args, userID)
		}
	}
	if len(query.UserIDs) > 0 {
		userIDs := make([]int64, 0, len(query.UserIDs))
		for _, raw := range query.UserIDs {
			userID, err := parseBillingUserID(raw)
			if err == nil {
				userIDs = append(userIDs, userID)
			}
		}
		if len(userIDs) > 0 {
			sqlText += fmt.Sprintf(" AND ul.user_id = ANY($%d)", len(args)+1)
			args = append(args, pq.Array(userIDs))
		}
	}
	return sqlText, args
}

func (s *BillingAPIService) zeroSummary() BillingSummary {
	exchange := s.exchangeRate
	return BillingSummary{
		Currency:     billingDefaultCurrency,
		ExchangeRate: &exchange,
	}
}

func (s *BillingAPIService) summaryFromParts(requests, input, output, cached, reasoning int64, costUSD float64) BillingSummary {
	exchange := s.exchangeRate
	total := input + output + cached + reasoning
	costCNY := costUSD * s.exchangeRate
	summary := BillingSummary{
		RequestCount:      requests,
		InputTokens:       input,
		OutputTokens:      output,
		TotalTokens:       total,
		CachedInputTokens: cached,
		ReasoningTokens:   reasoning,
		CostUSD:           roundBilling(costUSD),
		CostCNY:           roundBilling(costCNY),
		Currency:          billingDefaultCurrency,
		ExchangeRate:      &exchange,
	}
	if total > 0 {
		usdAvg := roundBilling(costUSD / float64(total) * 1_000_000)
		cnyAvg := roundBilling(costCNY / float64(total) * 1_000_000)
		summary.AverageCostUSDPer1MTokens = &usdAvg
		summary.AverageCostCNYPer1MTokens = &cnyAvg
	}
	return summary
}

func (s *BillingAPIService) freshnessFromLastSynced(lastSynced sql.NullTime) BillingFreshness {
	freshness := DefaultBillingFreshness()
	if lastSynced.Valid {
		last := lastSynced.Time.UnixMilli()
		next := lastSynced.Time.Add(5 * time.Minute).UnixMilli()
		delay := int64(math.Max(0, s.now().Sub(lastSynced.Time).Seconds()))
		freshness.LastSyncedAtMS = &last
		freshness.NextSyncAvailableMS = &next
		freshness.DataDelaySeconds = &delay
	}
	return freshness
}

func parseBillingProviderKeyID(raw string) (int64, error) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, billingProviderKeyPrefix) {
		return 0, newBillingError(400, BillingErrInvalidProviderKey, "provider_key_id is invalid.", map[string]any{"provider_key_id": raw})
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(trimmed, billingProviderKeyPrefix), 10, 64)
	if err != nil || id <= 0 {
		return 0, newBillingError(400, BillingErrInvalidProviderKey, "provider_key_id is invalid.", map[string]any{"provider_key_id": raw})
	}
	return id, nil
}

func parseBillingUserID(raw string) (int64, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "user_")
	id, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || id <= 0 {
		return 0, newBillingError(400, BillingErrInvalidArgument, "user_id is invalid.", map[string]any{"user_id": raw})
	}
	return id, nil
}

func formatBillingProviderKeyID(id int64) string {
	return fmt.Sprintf("%s%d", billingProviderKeyPrefix, id)
}

func formatBillingSourceProviderID(id int64) string {
	return fmt.Sprintf("api_key_%d", id)
}

func maskBillingProviderKey(key string) string {
	last := last4(key)
	if last == "" {
		return ""
	}
	prefix := ""
	if idx := strings.Index(key, "-"); idx > 0 {
		prefix = key[:idx+1]
	}
	return prefix + "..." + last
}

func fingerprintKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}

func last4(key string) string {
	if len(key) <= 4 {
		return key
	}
	return key[len(key)-4:]
}

func billingKeyStatus(status string, expiresAt sql.NullTime) string {
	if expiresAt.Valid && time.Now().After(expiresAt.Time) {
		return "expired"
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusActive:
		return "active"
	case StatusDisabled, "inactive":
		return "disabled"
	case StatusExpired:
		return "expired"
	case StatusError:
		return "invalid"
	default:
		return "unknown"
	}
}

func validateBillingRange(startMS, endMS int64, granularity string) error {
	if startMS <= 0 || endMS <= startMS {
		return newBillingError(400, BillingErrInvalidArgument, "start_ms and end_ms are required and must form a valid half-open range.", nil)
	}
	duration := time.Duration(endMS-startMS) * time.Millisecond
	switch granularity {
	case "", "hour":
		if duration > 31*24*time.Hour {
			return newBillingError(400, BillingErrTimeRangeTooLarge, "The requested time range exceeds the maximum allowed range for hour granularity.", map[string]any{"max_days": 31, "granularity": "hour"})
		}
	case "day":
		if duration > 366*24*time.Hour {
			return newBillingError(400, BillingErrTimeRangeTooLarge, "The requested time range exceeds the maximum allowed range for day granularity.", map[string]any{"max_days": 366, "granularity": "day"})
		}
	case "month":
		if duration > 36*31*24*time.Hour {
			return newBillingError(400, BillingErrTimeRangeTooLarge, "The requested time range exceeds the maximum allowed range for month granularity.", map[string]any{"max_months": 36, "granularity": "month"})
		}
	default:
		return newBillingError(400, BillingErrUnsupportedGranularity, "Requested granularity is not supported.", map[string]any{"granularity": granularity})
	}
	return nil
}

type billingBucket struct {
	start time.Time
	end   time.Time
}

func buildBillingBuckets(startMS, endMS int64, granularity string, loc *time.Location) []billingBucket {
	start := time.UnixMilli(startMS).In(loc)
	end := time.UnixMilli(endMS).In(loc)
	switch granularity {
	case "hour":
		start = time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), 0, 0, 0, loc)
	case "month":
		start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, loc)
	default:
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	}
	var buckets []billingBucket
	for cur := start; cur.Before(end); {
		next := cur.AddDate(0, 0, 1)
		if granularity == "hour" {
			next = cur.Add(time.Hour)
		} else if granularity == "month" {
			next = cur.AddDate(0, 1, 0)
		}
		buckets = append(buckets, billingBucket{start: cur, end: next})
		cur = next
	}
	return buckets
}

func isSupportedBillingDimension(dimension string) bool {
	switch dimension {
	case "model", "provider_key", "provider_platform", "user", "day", "hour":
		return true
	default:
		return false
	}
}

func billingDimensionExpressions(dimension string) (string, string) {
	switch dimension {
	case "provider_key":
		return "CAST(ul.api_key_id AS TEXT)", "CAST(ul.api_key_id AS TEXT)"
	case "provider_platform":
		expr := "COALESCE(NULLIF(COALESCE(NULLIF(g.platform, ''), a.platform), ''), 'unknown')"
		return expr, expr
	case "user":
		return "CAST(ul.user_id AS TEXT)", "COALESCE(NULLIF(u.username, ''), u.email, CAST(ul.user_id AS TEXT))"
	case "day":
		expr := "to_char(ul.created_at, 'YYYY-MM-DD')"
		return expr, expr
	case "hour":
		expr := "to_char(ul.created_at, 'YYYY-MM-DD HH24:00')"
		return expr, expr
	default:
		expr := "COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model)"
		return expr, expr
	}
}

func normalizeBillingLimit(limit int) int {
	if limit <= 0 {
		return billingDefaultLimit
	}
	if limit > billingMaxLimit {
		return billingMaxLimit
	}
	return limit
}

func parseBillingCursor(cursor string) int {
	cursor = strings.TrimSpace(cursor)
	if strings.HasPrefix(cursor, "offset:") {
		if n, err := strconv.Atoi(strings.TrimPrefix(cursor, "offset:")); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func percentage(part, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return roundBilling(part / total * 100)
}

func roundBilling(v float64) float64 {
	return math.Round(v*1e8) / 1e8
}

func uniqueInt64(in []int64) []int64 {
	seen := make(map[int64]struct{}, len(in))
	out := make([]int64, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func encodeBillingJobID(prefix string, query BillingQuery) string {
	b, _ := json.Marshal(query)
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b)
}

func decodeBillingJobID(prefix, id string) (BillingQuery, error) {
	if !strings.HasPrefix(id, prefix+"_") {
		return BillingQuery{}, errors.New("invalid id")
	}
	raw := strings.TrimPrefix(id, prefix+"_")
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return BillingQuery{}, err
	}
	var query BillingQuery
	if err := json.Unmarshal(b, &query); err != nil {
		return BillingQuery{}, err
	}
	return query, nil
}

type billingExportPayload struct {
	Query     BillingQuery `json:"query"`
	Format    string       `json:"format"`
	Level     string       `json:"level"`
	Dimension string       `json:"dimension,omitempty"`
}

func encodeBillingExportID(payload billingExportPayload) string {
	b, _ := json.Marshal(payload)
	return "export_" + base64.RawURLEncoding.EncodeToString(b)
}

func decodeBillingExportID(id string) (billingExportPayload, error) {
	if !strings.HasPrefix(id, "export_") {
		return billingExportPayload{}, errors.New("invalid export id")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "export_"))
	if err != nil {
		return billingExportPayload{}, err
	}
	var payload billingExportPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return billingExportPayload{}, err
	}
	return payload, nil
}
