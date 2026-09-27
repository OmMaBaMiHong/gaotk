//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Product branding must not override an active subscription's model access.
func TestSkoobPlanSubscriptionAllowsKeyBinding(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	groups := newGroupRepositoryWithSQL(tx.Client(), tx)
	keys := newAPIKeyRepositoryWithSQL(tx.Client(), tx)
	users := newUserRepositoryWithSQL(tx.Client(), tx)
	subs := NewUserSubscriptionRepository(tx.Client())
	user, err := tx.Client().User.Create().SetEmail("skoob-binding@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	svc := service.NewAPIKeyService(keys, users, groups, subs, nil, nil, &config.Config{})
	for _, tc := range []struct {
		name, product string
		forSale       bool
	}{
		{"monthly", "skoob", true},
		{"retired", " Skoob ", false},
		{"other", "another-product", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &service.Group{Name: tc.name, Platform: service.PlatformComposite, IsExclusive: true, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription}
			require.NoError(t, groups.Create(ctx, g))
			_, err := tx.Client().SubscriptionPlan.Create().SetGroupID(g.ID).SetName(tc.name).SetPrice(1).SetProductName(tc.product).SetForSale(tc.forSale).Save(ctx)
			require.NoError(t, err)
			req := service.CreateAPIKeyRequest{Name: tc.name, GroupID: &g.ID}
			_, err = svc.Create(ctx, user.ID, req)
			require.ErrorIs(t, err, service.ErrGroupNotAllowed, "membership product alone grants no access")
			sub := &service.UserSubscription{UserID: user.ID, GroupID: g.ID, Status: service.SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}
			require.NoError(t, subs.Create(ctx, sub))
			available, err := svc.GetAvailableGroups(ctx, user.ID)
			require.NoError(t, err)
			require.True(t, containsSubscriptionGroup(available, g.ID))
			key, err := svc.Create(ctx, user.ID, req)
			require.NoError(t, err)
			require.Equal(t, g.ID, *key.GroupID)
			updated, err := svc.Update(ctx, key.ID, user.ID, service.UpdateAPIKeyRequest{GroupID: &g.ID})
			require.NoError(t, err)
			require.Equal(t, g.ID, *updated.GroupID)
			auth, err := keys.GetByKeyForAuth(ctx, key.Key)
			require.NoError(t, err)
			require.True(t, auth.Group.IsSubscriptionType())
			require.Equal(t, service.PlatformComposite, auth.Group.Platform)
			// Expiration still removes the group and prevents both binding paths.
			_, err = tx.Client().UserSubscription.UpdateOneID(sub.ID).SetExpiresAt(time.Now().Add(-time.Hour)).Save(ctx)
			require.NoError(t, err)
			available, err = svc.GetAvailableGroups(ctx, user.ID)
			require.NoError(t, err)
			require.False(t, containsSubscriptionGroup(available, g.ID))
			_, err = svc.Create(ctx, user.ID, req)
			require.ErrorIs(t, err, service.ErrGroupNotAllowed)
			_, err = svc.Update(ctx, key.ID, user.ID, service.UpdateAPIKeyRequest{GroupID: &g.ID})
			require.ErrorIs(t, err, service.ErrGroupNotAllowed)
		})
	}
}

func containsSubscriptionGroup(groups []service.Group, id int64) bool {
	for _, group := range groups {
		if group.ID == id {
			return true
		}
	}
	return false
}
