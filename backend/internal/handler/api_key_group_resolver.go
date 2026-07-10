package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func resolveEffectiveAPIKeyGroupForRequest(ctx context.Context, matcher service.ModelGroupMatcher, apiKey *service.APIKey, model string) (*int64, error) {
	groupID, err := service.ResolveEffectiveAPIKeyGroup(ctx, matcher, apiKey, model)
	if err != nil {
		return nil, err
	}
	applyEffectiveAPIKeyGroup(apiKey, groupID)
	return groupID, nil
}

func resolveEffectiveAPIKeyGroupForPlatform(ctx context.Context, matcher service.ModelGroupMatcher, apiKey *service.APIKey, model, platform string) (*int64, error) {
	if platform == "" {
		return resolveEffectiveAPIKeyGroupForRequest(ctx, matcher, apiKey, model)
	}
	groupID, err := service.ResolveEffectiveAPIKeyGroup(ctx, platformModelGroupMatcher{
		base:     matcher,
		apiKey:   apiKey,
		platform: platform,
	}, apiKey, model)
	if err != nil {
		return nil, err
	}
	applyEffectiveAPIKeyGroup(apiKey, groupID)
	return groupID, nil
}

func applyEffectiveAPIKeyGroup(apiKey *service.APIKey, groupID *int64) {
	if apiKey == nil || groupID == nil {
		return
	}
	apiKey.GroupID = groupID
	if apiKey.Group != nil && apiKey.Group.ID == *groupID {
		return
	}
	for i := range apiKey.Groups {
		if apiKey.Groups[i].ID == *groupID {
			group := apiKey.Groups[i]
			apiKey.Group = &group
			return
		}
	}
}

type platformModelGroupMatcher struct {
	base     service.ModelGroupMatcher
	apiKey   *service.APIKey
	platform string
}

func (m platformModelGroupMatcher) GroupSupportsModel(ctx context.Context, groupID int64, model string) (bool, error) {
	group := findAPIKeyGroupByID(m.apiKey, groupID)
	if group == nil || group.Platform != m.platform {
		return false, nil
	}
	if m.base == nil {
		return true, nil
	}
	return m.base.GroupSupportsModel(ctx, groupID, model)
}

func findAPIKeyGroupByID(apiKey *service.APIKey, groupID int64) *service.Group {
	if apiKey == nil {
		return nil
	}
	if apiKey.Group != nil && apiKey.Group.ID == groupID {
		return apiKey.Group
	}
	for i := range apiKey.Groups {
		if apiKey.Groups[i].ID == groupID {
			return &apiKey.Groups[i]
		}
	}
	return nil
}
