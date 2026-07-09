package service

import (
	"context"
	"fmt"
)

type ModelGroupMatcher interface {
	GroupSupportsModel(ctx context.Context, groupID int64, model string) (bool, error)
}

func ResolveEffectiveAPIKeyGroup(ctx context.Context, matcher ModelGroupMatcher, apiKey *APIKey, model string) (*int64, error) {
	if apiKey == nil {
		return nil, fmt.Errorf("api key is required")
	}
	groupIDs := normalizeAPIKeyGroupIDs(apiKey.GroupID, apiKey.GroupIDs)
	if len(groupIDs) == 0 {
		return nil, fmt.Errorf("api key is not bound to a group")
	}
	if len(groupIDs) == 1 || matcher == nil {
		groupID := groupIDs[0]
		return &groupID, nil
	}
	for _, groupID := range groupIDs {
		ok, err := matcher.GroupSupportsModel(ctx, groupID, model)
		if err != nil {
			return nil, err
		}
		if ok {
			resolved := groupID
			return &resolved, nil
		}
	}
	if apiKey.GroupID != nil {
		return apiKey.GroupID, nil
	}
	return nil, fmt.Errorf("no bound group supports model %q", model)
}
