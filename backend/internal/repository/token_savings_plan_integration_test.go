//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTokenSavingsPlanSnapshotIntegration(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	owner := mustCreateUser(t, client, &service.User{})
	admin := mustCreateUser(t, client, &service.User{Role: service.RoleAdmin})
	group := mustCreateGroup(t, client, &service.Group{Name: "pro20-" + uuid.NewString(), Platform: service.PlatformOpenAI})
	channel := &service.Channel{Name: "pro20-" + uuid.NewString(), Status: service.StatusActive, GroupIDs: []int64{group.ID}, FeaturesConfig: map[string]any{"token_savings": service.TokenSavingsConfig{
		Enabled: true, OwnerShareBPS: 8000, AdminUserID: admin.ID, ReceivingGroupIDs: []int64{group.ID}, ReceivingRules: []service.SavingsReceivingRule{{GroupID: group.ID, Priority: 100, AllowedPlans: []string{"pro"}}},
	}}}
	require.NoError(t, (&channelRepository{db: integrationDB}).Create(ctx, channel))
	repo := NewAccountRepository(client, integrationDB, nil)
	parent := &service.Account{Name: "verified owner", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, OwnerUserID: &owner.ID, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"plan_type": "pro", "savings_verified_plan_type": "pro", "access_token": "test-only"}}
	require.NoError(t, repo.(service.AccountDuplicateRepository).CreateWithAccountGroups(ctx, parent, []service.AccountGroup{{GroupID: group.ID}}))
	shadow := &service.Account{Name: "verified shadow", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ParentAccountID: &parent.ID, QuotaDimension: "spark", Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{}}
	require.NoError(t, repo.(service.AccountDuplicateRepository).CreateWithAccountGroups(ctx, shadow, []service.AccountGroup{{GroupID: group.ID}}))
	for _, id := range []int64{parent.ID, shadow.ID} {
		account, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "pro", account.Rental.OwnerCurrentPlan)
		require.Equal(t, "pro", account.Rental.OwnerVerifiedPlan)
		require.True(t, account.IsTokenSavingsSchedulableForGroup(&group.ID))
	}
	parent.Credentials["plan_type"] = "prolite"
	require.NoError(t, repo.Update(ctx, parent))
	for _, id := range []int64{parent.ID, shadow.ID} {
		account, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		require.False(t, account.IsSchedulable())
		require.False(t, account.IsTokenSavingsSchedulableForGroup(&group.ID))
	}
}
