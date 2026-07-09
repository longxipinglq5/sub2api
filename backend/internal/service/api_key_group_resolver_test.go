//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveEffectiveAPIKeyGroup_SingleLegacyGroup(t *testing.T) {
	groupID := int64(10)
	key := &APIKey{ID: 1, GroupID: &groupID}

	got, err := ResolveEffectiveAPIKeyGroup(context.Background(), nil, key, "gpt-5")

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, int64(10), *got)
}

func TestResolveEffectiveAPIKeyGroup_ChoosesFirstMatchingGroup(t *testing.T) {
	key := &APIKey{ID: 1, GroupIDs: []int64{10, 20}}
	resolver := fakeModelGroupMatcher{matches: map[int64]bool{20: true}}

	got, err := ResolveEffectiveAPIKeyGroup(context.Background(), resolver, key, "qwen-plus")

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, int64(20), *got)
}

func TestResolveEffectiveAPIKeyGroup_ReturnsForbiddenWhenNoGroupMatches(t *testing.T) {
	key := &APIKey{ID: 1, GroupIDs: []int64{10, 20}}
	resolver := fakeModelGroupMatcher{matches: map[int64]bool{}}

	got, err := ResolveEffectiveAPIKeyGroup(context.Background(), resolver, key, "glm-4.6")

	require.Nil(t, got)
	require.ErrorContains(t, err, "no bound group supports model")
}

func TestResolveEffectiveAPIKeyGroup_FallsBackToLegacyGroupWhenNoGroupMatches(t *testing.T) {
	groupID := int64(10)
	key := &APIKey{ID: 1, GroupID: &groupID, GroupIDs: []int64{20}}
	resolver := fakeModelGroupMatcher{matches: map[int64]bool{}}

	got, err := ResolveEffectiveAPIKeyGroup(context.Background(), resolver, key, "legacy-model")

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, int64(10), *got)
}

func TestResolveEffectiveAPIKeyGroup_ReturnsMatcherError(t *testing.T) {
	key := &APIKey{ID: 1, GroupIDs: []int64{10, 20}}
	resolver := fakeModelGroupMatcher{err: errors.New("matcher failed")}

	got, err := ResolveEffectiveAPIKeyGroup(context.Background(), resolver, key, "qwen-plus")

	require.Nil(t, got)
	require.ErrorContains(t, err, "matcher failed")
}

type fakeModelGroupMatcher struct {
	matches map[int64]bool
	err     error
}

func (f fakeModelGroupMatcher) GroupSupportsModel(_ context.Context, groupID int64, _ string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.matches[groupID], nil
}
