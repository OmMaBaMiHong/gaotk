//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func savingsScheduleAccount() *Account {
	owner := int64(1)
	return &Account{ID: 10, OwnerUserID: &owner, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"plan_type": "pro", "savings_verified_plan_type": "pro"},
		Rental: &RentalSnapshot{OwnerUserID: owner, Platform: PlatformOpenAI, OwnerVerifiedPlan: "pro", OwnerCurrentPlan: "pro", Channels: map[int64]*RentalSnapshot{
			11: {Enabled: true, AllowedPlans: []string{"pro"}}, 12: {Enabled: true, AllowedPlans: []string{"prolite"}},
		}},
	}
}

func TestSavingsSchedulingRequiresVerifiedExactPlanForActualGroup(t *testing.T) {
	a := savingsScheduleAccount()
	groupID := int64(11)
	require.True(t, a.IsTokenSavingsSchedulableForGroup(&groupID))
	groupID = 12
	require.False(t, a.IsTokenSavingsSchedulableForGroup(&groupID), "eligibility of another group cannot authorize this one")
	groupID = 11
	for _, plan := range []string{"", "prolite", "plus", "free", "team", "selfservebusinessprolite"} {
		a.Rental.OwnerCurrentPlan = plan
		require.False(t, a.IsTokenSavingsSchedulableForGroup(&groupID), plan)
	}
	a.Rental.OwnerCurrentPlan = "pro"
	a.Rental.OwnerVerifiedPlan = ""
	require.False(t, a.IsTokenSavingsSchedulableForGroup(&groupID))
	a.Rental = nil
	require.False(t, a.IsTokenSavingsSchedulableForGroup(&groupID), "legacy owner without verified snapshot fails closed")
	a.OwnerUserID = nil
	require.True(t, a.IsTokenSavingsSchedulableForGroup(&groupID), "original admin accounts remain unchanged")
	a = savingsScheduleAccount()
	a.Platform = PlatformDeepseek
	require.True(t, a.IsTokenSavingsSchedulableForGroup(&groupID))
}

func TestSavingsGuardCoversSchedulerAndFreshRecheck(t *testing.T) {
	ctx := context.Background()
	a := savingsScheduleAccount()
	groupID := int64(12)
	scheduler := &defaultOpenAIAccountScheduler{}
	ok, reason := scheduler.isAccountRequestCompatibleReason(ctx, a, OpenAIAccountScheduleRequest{GroupID: &groupID, Platform: PlatformOpenAI})
	require.False(t, ok)
	require.Equal(t, "savings_plan_not_allowed", reason)
	svc := &OpenAIGatewayService{}
	require.Nil(t, svc.recheckSelectedOpenAIAccountFromDBBeforeProfit(ctx, a, &groupID, PlatformOpenAI, "", false, ""))
	require.False(t, svc.openAIAccountMatchesSchedulingGroup(a, &groupID))
}

func TestSavingsShadowRechecksCurrentParentPlanEvenWithCachedSnapshot(t *testing.T) {
	parent := savingsScheduleAccount()
	shadow := savingsScheduleAccount()
	shadow.ParentAccountID = &parent.ID
	shadow.QuotaDimension = "spark"
	shadow.Credentials = map[string]any{}
	parent.Credentials["plan_type"] = "prolite"
	require.False(t, parentHealthyForShadow(shadow, func(int64) *Account { return parent }))
	parent.Credentials["plan_type"] = "pro"
	parent.Credentials["savings_verified_plan_type"] = ""
	require.False(t, parentHealthyForShadow(shadow, func(int64) *Account { return parent }))
}

func TestSavingsTerminalGateRechecksWSTurnWithoutProfitGate(t *testing.T) {
	selected := savingsScheduleAccount()
	latest := savingsScheduleAccount()
	latest.Credentials["plan_type"] = "prolite"
	repo := &refreshAPIAccountRepo{account: latest}
	svc := &OpenAIGatewayService{accountRepo: repo}
	groupID := int64(11)
	_, vetoed, reason := svc.ProfitControlVetoLatest(context.Background(), selected, &groupID)
	require.True(t, vetoed)
	require.Equal(t, "savings_plan_not_allowed", reason)
}
