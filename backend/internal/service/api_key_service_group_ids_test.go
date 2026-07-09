//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type apiKeyGroupIDsRepoStub struct {
	apiKeyRepoStub
	nextID      int64
	createdKeys []APIKey
	updatedKeys []APIKey
	setCalls    []apiKeyGroupIDsSetCall
}

type apiKeyGroupIDsSetCall struct {
	apiKeyID int64
	groupIDs []int64
}

func (s *apiKeyGroupIDsRepoStub) Create(_ context.Context, key *APIKey) error {
	if s.nextID == 0 {
		s.nextID = 100
	}
	key.ID = s.nextID
	s.nextID++
	s.createdKeys = append(s.createdKeys, *key)
	return nil
}

func (s *apiKeyGroupIDsRepoStub) GetByID(_ context.Context, id int64) (*APIKey, error) {
	if s.apiKey == nil {
		return nil, ErrAPIKeyNotFound
	}
	clone := *s.apiKey
	clone.ID = id
	return &clone, nil
}

func (s *apiKeyGroupIDsRepoStub) ExistsByKey(context.Context, string) (bool, error) {
	return false, nil
}

func (s *apiKeyGroupIDsRepoStub) Update(_ context.Context, key *APIKey) error {
	s.updatedKeys = append(s.updatedKeys, *key)
	return nil
}

func (s *apiKeyGroupIDsRepoStub) SetGroupIDs(_ context.Context, apiKeyID int64, groupIDs []int64) error {
	s.setCalls = append(s.setCalls, apiKeyGroupIDsSetCall{
		apiKeyID: apiKeyID,
		groupIDs: append([]int64(nil), groupIDs...),
	})
	return nil
}

type apiKeyGroupIDsGroupRepoStub struct {
	groupRepoStub
	groups map[int64]*Group
}

func (s *apiKeyGroupIDsGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	group, ok := s.groups[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	clone := *group
	return &clone, nil
}

func TestAPIKeyService_Create_WithGroupIDsStoresPrimaryAndBindings(t *testing.T) {
	ctx := context.Background()
	repo := &apiKeyGroupIDsRepoStub{nextID: 101}
	userRepo := &userRepoStub{user: &User{ID: 7, AllowedGroups: []int64{10, 20}}}
	groupRepo := &apiKeyGroupIDsGroupRepoStub{groups: map[int64]*Group{
		10: {ID: 10, Status: StatusActive},
		20: {ID: 20, Status: StatusActive},
	}}
	svc := NewAPIKeyService(repo, userRepo, groupRepo, nil, nil, nil, nil)
	customKey := "sk-test-multi-groups-create"

	got, err := svc.Create(ctx, 7, CreateAPIKeyRequest{
		Name:      "multi",
		CustomKey: &customKey,
		GroupIDs:  []int64{10, 20},
	})

	require.NoError(t, err)
	require.NotNil(t, got.GroupID)
	require.Equal(t, int64(10), *got.GroupID)
	require.Equal(t, []int64{10, 20}, got.GroupIDs)
	require.Len(t, repo.createdKeys, 1)
	require.NotNil(t, repo.createdKeys[0].GroupID)
	require.Equal(t, int64(10), *repo.createdKeys[0].GroupID)
	require.Equal(t, []apiKeyGroupIDsSetCall{{apiKeyID: 101, groupIDs: []int64{10, 20}}}, repo.setCalls)
}

func TestAPIKeyService_Update_WithGroupIDsReplacesBindings(t *testing.T) {
	ctx := context.Background()
	repo := &apiKeyGroupIDsRepoStub{apiKeyRepoStub: apiKeyRepoStub{apiKey: &APIKey{
		ID:       55,
		UserID:   7,
		Key:      "sk-test-multi-groups-update",
		Name:     "old",
		Status:   StatusActive,
		GroupIDs: []int64{10, 20},
	}}}
	userRepo := &userRepoStub{user: &User{ID: 7, AllowedGroups: []int64{30}}}
	groupRepo := &apiKeyGroupIDsGroupRepoStub{groups: map[int64]*Group{
		30: {ID: 30, Status: StatusActive},
	}}
	svc := NewAPIKeyService(repo, userRepo, groupRepo, nil, nil, nil, nil)

	got, err := svc.Update(ctx, 55, 7, UpdateAPIKeyRequest{
		GroupIDs:    []int64{30},
		GroupIDsSet: true,
	})

	require.NoError(t, err)
	require.NotNil(t, got.GroupID)
	require.Equal(t, int64(30), *got.GroupID)
	require.Equal(t, []int64{30}, got.GroupIDs)
	require.Len(t, repo.updatedKeys, 1)
	require.NotNil(t, repo.updatedKeys[0].GroupID)
	require.Equal(t, int64(30), *repo.updatedKeys[0].GroupID)
	require.Equal(t, []apiKeyGroupIDsSetCall{{apiKeyID: 55, groupIDs: []int64{30}}}, repo.setCalls)
}
