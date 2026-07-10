//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestResolveEffectiveAPIKeyGroupForRequest_AppliesMatchedGroup(t *testing.T) {
	legacyGroupID := int64(10)
	apiKey := &service.APIKey{
		ID:       1,
		GroupID:  &legacyGroupID,
		GroupIDs: []int64{10, 20},
		Group:    &service.Group{ID: 10, Platform: service.PlatformOpenAI},
		Groups: []service.Group{
			{ID: 10, Platform: service.PlatformOpenAI},
			{ID: 20, Platform: service.PlatformQwen},
		},
	}

	got, err := resolveEffectiveAPIKeyGroupForRequest(context.Background(), handlerFakeModelGroupMatcher{matches: map[int64]bool{20: true}}, apiKey, "qwen-plus")

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, int64(20), *got)
	require.NotNil(t, apiKey.GroupID)
	require.Equal(t, int64(20), *apiKey.GroupID)
	require.NotNil(t, apiKey.Group)
	require.Equal(t, service.PlatformQwen, apiKey.Group.Platform)
}

func TestResolveEffectiveAPIKeyGroupForPlatform_SkipsOtherPlatforms(t *testing.T) {
	apiKey := &service.APIKey{
		ID:       1,
		GroupIDs: []int64{10, 20},
		Groups: []service.Group{
			{ID: 10, Platform: service.PlatformOpenAI},
			{ID: 20, Platform: service.PlatformGemini},
		},
	}

	got, err := resolveEffectiveAPIKeyGroupForPlatform(context.Background(), handlerFakeModelGroupMatcher{matches: map[int64]bool{10: true, 20: true}}, apiKey, "gemini-2.5-pro", service.PlatformGemini)

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, int64(20), *got)
	require.NotNil(t, apiKey.Group)
	require.Equal(t, service.PlatformGemini, apiKey.Group.Platform)
}

func TestResolveEffectiveAPIKeyGroupForPlatform_PrimaryQwenSelectsOpenAIGroup(t *testing.T) {
	qwenGroupID := int64(5)
	apiKey := &service.APIKey{
		ID:       1,
		GroupID:  &qwenGroupID,
		GroupIDs: []int64{5, 2},
		Group:    &service.Group{ID: 5, Platform: service.PlatformQwen},
		Groups: []service.Group{
			{ID: 5, Platform: service.PlatformQwen},
			{ID: 2, Platform: service.PlatformOpenAI},
		},
	}

	got, err := resolveEffectiveAPIKeyGroupForPlatform(context.Background(), handlerFakeModelGroupMatcher{matches: map[int64]bool{5: true, 2: true}}, apiKey, "gpt-5.5", service.PlatformOpenAI)

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, int64(2), *got)
	require.NotNil(t, apiKey.GroupID)
	require.Equal(t, int64(2), *apiKey.GroupID)
	require.NotNil(t, apiKey.Group)
	require.Equal(t, service.PlatformOpenAI, apiKey.Group.Platform)
}

type handlerFakeModelGroupMatcher struct {
	matches map[int64]bool
}

func (m handlerFakeModelGroupMatcher) GroupSupportsModel(_ context.Context, groupID int64, _ string) (bool, error) {
	return m.matches[groupID], nil
}
