package service

import (
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBuildOpenAIRealtimeWSURL(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": "https://api.example.test/v1/responses",
	}}
	incoming := url.Values{"model": {"ignored"}, "voice": {"alloy"}, "api_key": {"client-secret"}}
	got, err := svc.buildOpenAIRealtimeWSURL(account, "mapped-realtime", incoming)
	require.NoError(t, err)
	require.Equal(t, "wss://api.example.test/v1/realtime?model=mapped-realtime&voice=alloy", got)
}

func TestIsOpenAIRealtimeTransientError(t *testing.T) {
	require.True(t, isOpenAIRealtimeTransientError([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"Selected model is at capacity"}}`), 400))
	require.True(t, isOpenAIRealtimeTransientError([]byte(`{"type":"error","error":{"type":"server_error"}}`), 502))
	require.False(t, isOpenAIRealtimeTransientError([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"model is invalid"}}`), 400))
}
