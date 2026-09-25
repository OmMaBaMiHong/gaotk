//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSkoobMembershipPurposeLoadedFromPlans(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	groups := newGroupRepositoryWithSQL(tx.Client(), tx)
	keys := newAPIKeyRepositoryWithSQL(tx.Client(), tx)
	user, err := tx.Client().User.Create().SetEmail("skoob-purpose@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	for i, tc := range []struct {
		name, product string
		forSale, want bool
	}{
		{"monthly", "skoob", true, true},
		{"retired", " Skoob ", false, true},
		{"openskoob-free", "", false, false},
		{"skoob-name-is-not-a-marker", "another-product", true, false},
	} {
		g := &service.Group{Name: tc.name, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription}
		require.NoError(t, groups.Create(ctx, g))
		if tc.product != "" {
			_, err = tx.Client().SubscriptionPlan.Create().SetGroupID(g.ID).SetName(tc.name).SetPrice(1).SetProductName(tc.product).SetForSale(tc.forSale).Save(ctx)
			require.NoError(t, err)
		}
		got, err := groups.GetByID(ctx, g.ID)
		require.NoError(t, err)
		require.Equal(t, tc.want, got.SkoobMembershipOnly, tc.name)
		active, err := groups.ListActive(ctx)
		require.NoError(t, err)
		found := false
		for _, candidate := range active {
			if candidate.ID == g.ID {
				found = true
				require.Equal(t, tc.want, candidate.SkoobMembershipOnly, tc.name)
			}
		}
		require.True(t, found)
		key := &service.APIKey{UserID: user.ID, GroupID: &g.ID, Key: fmt.Sprintf("sk-skoob-purpose-%d", i), Name: tc.name, Status: service.StatusActive}
		require.NoError(t, keys.Create(ctx, key))
		auth, err := keys.GetByKeyForAuth(ctx, key.Key)
		require.NoError(t, err)
		require.Equal(t, tc.want, auth.Group.SkoobMembershipOnly, tc.name)
	}
}
