//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type hybridBindUserSubRepoStub struct {
	userSubRepoNoop
}

func (s hybridBindUserSubRepoStub) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func TestAPIKeyServiceCanUserBindGroup_HybridAllowsBalanceFallback(t *testing.T) {
	svc := NewAPIKeyService(nil, nil, nil, hybridBindUserSubRepoStub{}, nil, nil, nil)

	user := &User{ID: 7, Balance: 10}
	group := &Group{ID: 42, SubscriptionType: SubscriptionTypeHybrid}

	require.True(t, svc.canUserBindGroup(context.Background(), user, group))
}
