//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAvailableGroupsExcludeSkoobMembershipEvenWithSubscription(t *testing.T) {
	svc := &APIKeyService{
		userRepo: &visibilityUserRepo{user: &User{ID: 1}},
		userSubRepo: &visibilitySubRepo{subscriptions: []UserSubscription{
			{UserID: 1, GroupID: 12, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)},
			{UserID: 1, GroupID: 17, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)},
			{UserID: 1, GroupID: 20, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)},
		}},
		groupRepo: &visibilityGroupRepo{groups: []Group{
			{ID: 12, SubscriptionType: SubscriptionTypeSubscription, SkoobMembershipOnly: true},
			{ID: 17, IsExclusive: true, SubscriptionType: SubscriptionTypeSubscription, SkoobMembershipOnly: true},
			{ID: 16, Name: "openskoob-free", SubscriptionType: SubscriptionTypeStandard},
			{ID: 20, SubscriptionType: SubscriptionTypeSubscription},
		}},
	}
	groups, err := svc.GetAvailableGroups(context.Background(), 1)
	require.NoError(t, err)
	ids := make([]int64, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	require.Equal(t, []int64{16, 20}, ids)
	// Membership visibility itself must remain intact.
	visible, _, err := svc.GetUserGroupVisibility(context.Background(), 1)
	require.NoError(t, err)
	require.Contains(t, visible, int64(12))
	require.Contains(t, visible, int64(17))
}

func TestSkoobMembershipSurvivesAuthCacheRoundtrip(t *testing.T) {
	svc := &APIKeyService{}
	key := &APIKey{ID: 1, UserID: 1, User: &User{ID: 1, Status: StatusActive}, Group: &Group{ID: 12, Status: StatusActive, Hydrated: true, SkoobMembershipOnly: true}}
	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), key)})
	require.NoError(t, err)
	var entry APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &entry))
	got, used, err := svc.applyAuthCacheEntry("test-key", &entry)
	require.NoError(t, err)
	require.True(t, used)
	require.True(t, got.Group.SkoobMembershipOnly)
	entry.Snapshot.Version = 24
	_, used, err = svc.applyAuthCacheEntry("test-key", &entry)
	require.NoError(t, err)
	require.False(t, used, "old caches without membership purpose must be reloaded")
}

type membershipBindingGroupRepo struct{ GroupRepository }

func (*membershipBindingGroupRepo) GetByID(context.Context, int64) (*Group, error) {
	return &Group{ID: 12, SubscriptionType: SubscriptionTypeSubscription, SkoobMembershipOnly: true}, nil
}

type membershipBindingKeyRepo struct{ APIKeyRepository }

func (*membershipBindingKeyRepo) GetByID(context.Context, int64) (*APIKey, error) {
	return &APIKey{ID: 1, UserID: 1, Status: StatusActive}, nil
}

func TestSkoobMembershipCannotBeBoundByCreateOrUpdate(t *testing.T) {
	svc := &APIKeyService{userRepo: &visibilityUserRepo{user: &User{ID: 1}}, groupRepo: &membershipBindingGroupRepo{}, apiKeyRepo: &membershipBindingKeyRepo{}}
	groupID := int64(12)
	_, err := svc.Create(context.Background(), 1, CreateAPIKeyRequest{Name: "test", GroupID: &groupID})
	require.ErrorIs(t, err, ErrMembershipOnlyGroup)
	_, err = svc.Update(context.Background(), 1, 1, UpdateAPIKeyRequest{GroupID: &groupID})
	require.ErrorIs(t, err, ErrMembershipOnlyGroup)
}
