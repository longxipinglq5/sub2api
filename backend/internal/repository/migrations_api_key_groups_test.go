package repository

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyGroupsMigrationIsPresent(t *testing.T) {
	body, err := migrations.FS.ReadFile("156_api_key_groups.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(body))
	require.Contains(t, sql, "create table if not exists api_key_groups")
	require.Contains(t, sql, "id bigserial primary key")
	require.Contains(t, sql, "api_key_id bigint not null")
	require.Contains(t, sql, "group_id bigint not null")
	require.Contains(t, sql, "priority integer not null default 100")
	require.Contains(t, sql, "unique (api_key_id, group_id)")
	require.Contains(t, sql, "idx_api_key_groups_group_id")
}
