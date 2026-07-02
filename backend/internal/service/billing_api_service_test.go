package service

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func newBillingAPITestService(t *testing.T) (*BillingAPIService, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)

	cleanup := func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}

	return NewBillingAPIService(db), mock, cleanup
}

func TestBillingAPIService_CapabilitiesAdvertisesMultiProviderKeyQueries(t *testing.T) {
	svc, _, cleanup := newBillingAPITestService(t)
	defer cleanup()

	result, err := svc.Capabilities(context.Background())
	require.NoError(t, err)
	require.True(t, result.Features["multi_key_query"])
}

type billingUnixMilliArg int64

func (a billingUnixMilliArg) Match(v driver.Value) bool {
	t, ok := v.(time.Time)
	return ok && t.UnixMilli() == int64(a)
}

func TestBillingAPIService_ListProviderKeysRedactsSecrets(t *testing.T) {
	svc, mock, cleanup := newBillingAPITestService(t)
	defer cleanup()

	now := time.Unix(1782828000, 0).UTC()
	rows := sqlmock.NewRows([]string{
		"id", "key", "name", "user_id", "owner_email", "owner_name", "status",
		"created_at", "last_used_at", "expires_at", "provider_platform",
		"earliest_available_at", "latest_available_at", "last_synced_at",
	}).AddRow(
		int64(42),
		"sk-live-secret-ABCD",
		"Production Key",
		int64(7),
		"alice@example.com",
		"Alice",
		StatusActive,
		now.Add(-24*time.Hour),
		now,
		nil,
		PlatformOpenAI,
		now.Add(-2*time.Hour),
		now,
		now,
	)

	mock.ExpectQuery("(?s)FROM api_keys ak").
		WithArgs(int64(7), "", "mine").
		WillReturnRows(rows)

	result, err := svc.ListProviderKeys(context.Background(), BillingAuthSubject{
		UserID: 7,
		Role:   RoleUser,
	}, BillingProviderKeysQuery{Scope: "mine"})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)

	item := result.Items[0]
	require.Equal(t, "pk_apikey_42", item.ProviderKeyID)
	require.Equal(t, PlatformOpenAI, item.ProviderPlatform)
	require.Equal(t, "Production Key", item.DisplayName)
	require.Equal(t, "sk-...ABCD", item.MaskedKey)
	require.Equal(t, "api_key_42", item.SourceProviderID)
	require.Equal(t, "Production Key", item.SourceProviderName)
	require.Equal(t, "ABCD", item.KeyLast4)
	require.NotEmpty(t, item.KeyFingerprint)
	require.Equal(t, "active", item.Status)

	body, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(body), "sk-live-secret-ABCD")
	require.NotContains(t, string(body), `"api_key":"`)
}

func TestBillingAPIService_SummaryAggregatesVisibleProviderKey(t *testing.T) {
	svc, mock, cleanup := newBillingAPITestService(t)
	defer cleanup()

	start := time.Unix(1782748800, 0).UTC()
	end := start.Add(24 * time.Hour)
	startMS := start.UnixMilli()
	endMS := end.UnixMilli()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, user_id, status, expires_at FROM api_keys")).
		WithArgs(pq.Array([]int64{42})).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "status", "expires_at"}).
			AddRow(int64(42), int64(7), StatusActive, nil))

	mock.ExpectQuery("(?s)SELECT\\s+COUNT\\(\\*\\).*FROM usage_logs ul").
		WithArgs(billingUnixMilliArg(startMS), billingUnixMilliArg(endMS), pq.Array([]int64{42})).
		WillReturnRows(sqlmock.NewRows([]string{
			"request_count", "input_tokens", "output_tokens", "cached_input_tokens",
			"reasoning_tokens", "cost_usd", "last_synced_at",
		}).AddRow(int64(2), int64(300), int64(70), int64(15), int64(0), 0.1234, end.Add(-time.Minute)))

	result, err := svc.Summary(context.Background(), BillingAuthSubject{UserID: 7, Role: RoleUser}, BillingQuery{
		ProviderKeyID: "pk_apikey_42",
		StartMS:       startMS,
		EndMS:         endMS,
		Timezone:      "Asia/Shanghai",
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Summary.RequestCount)
	require.Equal(t, int64(300), result.Summary.InputTokens)
	require.Equal(t, int64(70), result.Summary.OutputTokens)
	require.Equal(t, int64(385), result.Summary.TotalTokens)
	require.Equal(t, int64(15), result.Summary.CachedInputTokens)
	require.InDelta(t, 0.1234, result.Summary.CostUSD, 1e-9)
	require.InDelta(t, 0.90082, result.Summary.CostCNY, 1e-9)
	require.Equal(t, "CNY", result.Summary.Currency)
	require.NotNil(t, result.Summary.ExchangeRate)
	require.Equal(t, "upstream_gateway", result.Freshness.Source)
}

func TestBillingAPIService_SummaryRejectsUnauthorizedProviderKey(t *testing.T) {
	svc, mock, cleanup := newBillingAPITestService(t)
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, user_id, status, expires_at FROM api_keys")).
		WithArgs(pq.Array([]int64{42})).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "status", "expires_at"}).
			AddRow(int64(42), int64(99), StatusActive, nil))

	_, err := svc.Summary(context.Background(), BillingAuthSubject{UserID: 7, Role: RoleUser}, BillingQuery{
		ProviderKeyID: "pk_apikey_42",
		StartMS:       time.Now().Add(-time.Hour).UnixMilli(),
		EndMS:         time.Now().UnixMilli(),
	})
	require.Error(t, err)

	var apiErr *BillingAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, BillingErrPermissionDenied, apiErr.Code)
}

func TestBillingAPIService_SummaryDefaultsToAllVisibleProviderKeys(t *testing.T) {
	svc, mock, cleanup := newBillingAPITestService(t)
	defer cleanup()

	start := time.Unix(1782748800, 0).UTC()
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM api_keys WHERE deleted_at IS NULL AND status = $1 AND user_id = $2")).
		WithArgs(StatusActive, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).
			AddRow(int64(42)).
			AddRow(int64(43)))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, user_id, status, expires_at FROM api_keys")).
		WithArgs(pq.Array([]int64{42, 43})).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "status", "expires_at"}).
			AddRow(int64(42), int64(7), StatusActive, nil).
			AddRow(int64(43), int64(7), StatusActive, nil))

	mock.ExpectQuery("(?s)SELECT\\s+COUNT\\(\\*\\).*FROM usage_logs ul").
		WithArgs(billingUnixMilliArg(start.UnixMilli()), billingUnixMilliArg(end.UnixMilli()), pq.Array([]int64{42, 43})).
		WillReturnRows(sqlmock.NewRows([]string{
			"request_count", "input_tokens", "output_tokens", "cached_input_tokens",
			"reasoning_tokens", "cost_usd", "last_synced_at",
		}).AddRow(int64(4), int64(500), int64(100), int64(0), int64(0), 1.25, end))

	result, err := svc.Summary(context.Background(), BillingAuthSubject{UserID: 7, Role: RoleUser}, BillingQuery{
		StartMS: start.UnixMilli(),
		EndMS:   end.UnixMilli(),
	})
	require.NoError(t, err)
	require.Equal(t, int64(4), result.Summary.RequestCount)
	require.Equal(t, int64(600), result.Summary.TotalTokens)
	require.Empty(t, result.Query.ProviderKeyID)
	require.Empty(t, result.Query.ProviderKeyIDs)
}

func TestBillingAPIService_SummaryAcceptsProviderKeyIDsArray(t *testing.T) {
	svc, mock, cleanup := newBillingAPITestService(t)
	defer cleanup()

	start := time.Unix(1782748800, 0).UTC()
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, user_id, status, expires_at FROM api_keys")).
		WithArgs(pq.Array([]int64{42, 43})).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "status", "expires_at"}).
			AddRow(int64(42), int64(7), StatusActive, nil).
			AddRow(int64(43), int64(7), StatusActive, nil))

	mock.ExpectQuery("(?s)SELECT\\s+COUNT\\(\\*\\).*FROM usage_logs ul").
		WithArgs(billingUnixMilliArg(start.UnixMilli()), billingUnixMilliArg(end.UnixMilli()), pq.Array([]int64{42, 43})).
		WillReturnRows(sqlmock.NewRows([]string{
			"request_count", "input_tokens", "output_tokens", "cached_input_tokens",
			"reasoning_tokens", "cost_usd", "last_synced_at",
		}).AddRow(int64(3), int64(300), int64(90), int64(0), int64(0), 0.75, end))

	result, err := svc.Summary(context.Background(), BillingAuthSubject{UserID: 7, Role: RoleUser}, BillingQuery{
		ProviderKeyIDs: []string{"pk_apikey_42", "pk_apikey_43"},
		StartMS:        start.UnixMilli(),
		EndMS:          end.UnixMilli(),
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), result.Summary.RequestCount)
	require.Equal(t, int64(390), result.Summary.TotalTokens)
}

func TestBillingAPIService_SummaryRejectsMixedProviderKeySelectors(t *testing.T) {
	svc, _, cleanup := newBillingAPITestService(t)
	defer cleanup()

	_, err := svc.Summary(context.Background(), BillingAuthSubject{UserID: 7, Role: RoleUser}, BillingQuery{
		ProviderKeyID:  "pk_apikey_42",
		ProviderKeyIDs: []string{"pk_apikey_43"},
		StartMS:        time.Now().Add(-time.Hour).UnixMilli(),
		EndMS:          time.Now().UnixMilli(),
	})
	require.Error(t, err)

	var apiErr *BillingAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, BillingErrInvalidArgument, apiErr.Code)
	require.Contains(t, apiErr.Message, "provider_key_id")
	require.Contains(t, apiErr.Message, "provider_key_ids")
}

func TestBillingAPIService_BreakdownGroupsByModel(t *testing.T) {
	svc, mock, cleanup := newBillingAPITestService(t)
	defer cleanup()

	start := time.Unix(1782748800, 0).UTC()
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, user_id, status, expires_at FROM api_keys")).
		WithArgs(pq.Array([]int64{42})).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "status", "expires_at"}).
			AddRow(int64(42), int64(7), StatusActive, nil))

	mock.ExpectQuery("(?s)SELECT\\s+COUNT\\(\\*\\).*FROM usage_logs ul").
		WithArgs(billingUnixMilliArg(start.UnixMilli()), billingUnixMilliArg(end.UnixMilli()), pq.Array([]int64{42})).
		WillReturnRows(sqlmock.NewRows([]string{
			"request_count", "input_tokens", "output_tokens", "cached_input_tokens",
			"reasoning_tokens", "cost_usd", "last_synced_at",
		}).AddRow(int64(3), int64(400), int64(100), int64(0), int64(0), 2.0, end))

	mock.ExpectQuery("(?s)GROUP BY dimension_key, dimension_label").
		WithArgs(billingUnixMilliArg(start.UnixMilli()), billingUnixMilliArg(end.UnixMilli()), pq.Array([]int64{42}), 21, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"dimension_key", "dimension_label", "request_count", "input_tokens", "output_tokens",
			"cached_input_tokens", "reasoning_tokens", "cost_usd",
		}).AddRow("gpt-5.5", "gpt-5.5", int64(2), int64(300), int64(80), int64(0), int64(0), 1.5))

	result, err := svc.Breakdown(context.Background(), BillingAuthSubject{UserID: 7, Role: RoleUser}, BillingBreakdownRequest{
		BillingQuery: BillingQuery{
			ProviderKeyID: "pk_apikey_42",
			StartMS:       start.UnixMilli(),
			EndMS:         end.UnixMilli(),
			Timezone:      "Asia/Shanghai",
			Limit:         20,
		},
		Dimension: "model",
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "model", result.Items[0].Dimension)
	require.Equal(t, "gpt-5.5", result.Items[0].Key)
	require.InDelta(t, 75.0, result.Items[0].PercentageOfTotalCost, 1e-9)
	require.InDelta(t, 76.0, result.Items[0].PercentageOfTotalTokens, 1e-9)
	require.False(t, result.HasMore)
}

func TestBillingAPIService_TimeseriesBucketsUseRequestedTimezone(t *testing.T) {
	svc, mock, cleanup := newBillingAPITestService(t)
	defer cleanup()

	start := time.Unix(1782748800, 0).UTC()
	end := start.Add(24 * time.Hour)
	bucketStart := time.Date(2026, 6, 30, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, user_id, status, expires_at FROM api_keys")).
		WithArgs(pq.Array([]int64{42})).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "status", "expires_at"}).
			AddRow(int64(42), int64(7), StatusActive, nil))

	mock.ExpectQuery("(?s)AT TIME ZONE \\$4.*GROUP BY bucket_start").
		WithArgs(billingUnixMilliArg(start.UnixMilli()), billingUnixMilliArg(end.UnixMilli()), pq.Array([]int64{42}), "Asia/Shanghai").
		WillReturnRows(sqlmock.NewRows([]string{
			"bucket_start", "request_count", "input_tokens", "output_tokens", "cached_input_tokens",
			"reasoning_tokens", "cost_usd", "last_synced_at",
		}).AddRow(bucketStart, int64(1), int64(100), int64(20), int64(0), int64(0), 0.5, bucketStart))

	result, err := svc.Timeseries(context.Background(), BillingAuthSubject{UserID: 7, Role: RoleUser}, BillingQuery{
		ProviderKeyID: "pk_apikey_42",
		StartMS:       start.UnixMilli(),
		EndMS:         end.UnixMilli(),
		Timezone:      "Asia/Shanghai",
		Granularity:   "day",
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, bucketStart.UnixMilli(), result.Items[0].BucketStartMS)
	require.Equal(t, int64(1), result.Items[0].RequestCount)
}

func TestBillingAPIService_SummaryFiltersByRequestedUser(t *testing.T) {
	svc, mock, cleanup := newBillingAPITestService(t)
	defer cleanup()

	start := time.Unix(1782748800, 0).UTC()
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, user_id, status, expires_at FROM api_keys")).
		WithArgs(pq.Array([]int64{42})).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "status", "expires_at"}).
			AddRow(int64(42), int64(7), StatusActive, nil))

	mock.ExpectQuery("(?s)ul\\.user_id = \\$4").
		WithArgs(billingUnixMilliArg(start.UnixMilli()), billingUnixMilliArg(end.UnixMilli()), pq.Array([]int64{42}), int64(8)).
		WillReturnRows(sqlmock.NewRows([]string{
			"request_count", "input_tokens", "output_tokens", "cached_input_tokens",
			"reasoning_tokens", "cost_usd", "last_synced_at",
		}).AddRow(int64(1), int64(80), int64(20), int64(0), int64(0), 0.25, end))

	result, err := svc.Summary(context.Background(), BillingAuthSubject{UserID: 7, Role: RoleAdmin}, BillingQuery{
		ProviderKeyID: "pk_apikey_42",
		StartMS:       start.UnixMilli(),
		EndMS:         end.UnixMilli(),
		UserID:        "user_8",
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Summary.RequestCount)
	require.Equal(t, int64(100), result.Summary.TotalTokens)
}
