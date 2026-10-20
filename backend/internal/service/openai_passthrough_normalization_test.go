package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeGPT6AstraPassthroughBody(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","input":"hello","temperature":0.7,"top_p":0.9,"top_logprobs":3,"reasoning":{"effort":"minimal"},"include":["reasoning.encrypted_content","message.output_text.logprobs"]}`)

	normalized, changed, err := normalizeGPT6AstraPassthroughBody(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(normalized, "temperature").Exists())
	require.False(t, gjson.GetBytes(normalized, "top_p").Exists())
	require.False(t, gjson.GetBytes(normalized, "top_logprobs").Exists())
	require.Equal(t, "low", gjson.GetBytes(normalized, "reasoning.effort").String())
	require.Equal(t, []string{"reasoning.encrypted_content"}, []string{gjson.GetBytes(normalized, "include.0").String()})
	require.Len(t, gjson.GetBytes(normalized, "include").Array(), 1)
}

func TestNormalizeOpenAIPassthroughOAuthBody_RemovesUnsupportedUser(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","user":"user_123","metadata":{"user_id":"user_123"},"prompt_cache_retention":"24h","safety_identifier":"sid","stream_options":{"include_usage":true}}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
	require.NoError(t, err)
	require.True(t, changed)
	for _, field := range openAIChatGPTInternalUnsupportedFields {
		require.False(t, gjson.GetBytes(normalized, field).Exists(), "%s should be stripped", field)
	}
	require.True(t, gjson.GetBytes(normalized, "stream").Bool())
	require.False(t, gjson.GetBytes(normalized, "store").Bool())
}

func TestNormalizeOpenAIPassthroughOAuthBody_CompactRemovesUnsupportedUser(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","user":"user_123","metadata":{"user_id":"user_123"},"stream":true,"store":true}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(normalized, "user").Exists())
	require.False(t, gjson.GetBytes(normalized, "metadata").Exists())
	require.False(t, gjson.GetBytes(normalized, "stream").Exists())
	require.False(t, gjson.GetBytes(normalized, "store").Exists())
}
