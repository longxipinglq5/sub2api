package service

import (
	"testing"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSV2UsageToOpenAIUsagePreservesCacheWriteProvenance(t *testing.T) {
	t.Parallel()

	got := openAIWSV2UsageToOpenAIUsage(openaiwsv2.Usage{
		InputTokens:                             20,
		AudioInputTokens:                        2,
		OutputTokens:                            4,
		AudioOutputTokens:                       1,
		CacheCreationInputTokens:                5,
		CacheCreationInputTokensIncludedInInput: true,
		CacheReadInputTokens:                    3,
		CacheReadAudioInputTokens:               1,
		ImageOutputTokens:                       7,
	})

	require.Equal(t, 20, got.InputTokens)
	require.Equal(t, 2, got.AudioInputTokens)
	require.Equal(t, 4, got.OutputTokens)
	require.Equal(t, 1, got.AudioOutputTokens)
	require.Equal(t, 5, got.CacheCreationInputTokens)
	require.True(t, got.CacheCreationInputTokensIncludedInInput)
	require.Equal(t, 3, got.CacheReadInputTokens)
	require.Equal(t, 1, got.CacheReadAudioInputTokens)
	require.Equal(t, 7, got.ImageOutputTokens)
}
