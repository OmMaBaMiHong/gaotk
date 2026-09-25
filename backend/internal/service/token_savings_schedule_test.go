//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"testing"

	"github.com/stretchr/testify/require"
)

func savingsScheduleAccount() *Account {
	owner := int64(1)
	return &Account{ID: 10, OwnerUserID: &owner, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"plan_type": "pro", "savings_verified_plan_type": "pro"},
		Rental: &RentalSnapshot{OwnerUserID: owner, OwnerAccountType: AccountTypeOAuth, Platform: PlatformOpenAI, OwnerVerifiedPlan: "pro", OwnerCurrentPlan: "pro", Channels: map[int64]*RentalSnapshot{
			11: {Enabled: true, AllowedAccountTypes: []string{AccountTypeOAuth}, AllowedPlans: []string{"pro"}}, 12: {Enabled: true, AllowedAccountTypes: []string{AccountTypeOAuth}, AllowedPlans: []string{"prolite"}},
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
	require.False(t, a.IsTokenSavingsSchedulableForGroup(&groupID), "platform mismatch fails closed")
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

func TestSavingsClaudeAndAPIKeyRuntimeRequireActualGroupType(t *testing.T) {
	a := savingsScheduleAccount()
	a.Platform = PlatformAnthropic
	a.Rental.Platform = PlatformAnthropic
	groupID := int64(11)
	svc := &GatewayService{}
	require.True(t, svc.isAccountSchedulableForModelSelection(context.Background(), a, "", &groupID))
	groupID = 12
	require.False(t, svc.isAccountSchedulableForModelSelection(context.Background(), a, "", &groupID))
	a.Type = AccountTypeAPIKey
	a.Rental.OwnerAccountType = AccountTypeAPIKey
	a.Credentials = map[string]any{"api_key": "test"}
	a.Rental.OwnerVerifiedPlan = ""
	a.Rental.OwnerCurrentPlan = ""
	groupID = 11
	require.False(t, a.IsTokenSavingsSchedulableForGroup(&groupID), "OAuth receiving does not admit API keys")
	a.Rental.Channels[11].AllowedAccountTypes = []string{AccountTypeAPIKey}
	a.Rental.Channels[11].AllowedPlans = nil
	require.True(t, a.IsTokenSavingsSchedulableForGroup(&groupID), "explicit API key opening needs no OAuth plan marker")
	a.Rental.Channels[11].AllowedAccountTypes = []string{}
	require.False(t, a.IsTokenSavingsSchedulableForGroup(&groupID))
	a.Rental.Channels[11].AllowedAccountTypes = nil
	require.False(t, a.IsTokenSavingsSchedulableForGroup(&groupID), "old incomplete cached snapshot fails closed")
	a.Rental.Channels[11].AllowedAccountTypes = []string{AccountTypeAPIKey}
	a.Rental.OwnerAccountType = AccountTypeOAuth
	require.False(t, a.IsTokenSavingsSchedulableForGroup(&groupID), "child type cannot override owner type")
}

func TestSavingsClaudeTerminalRechecksLatestWithoutProfitGate(t *testing.T) {
	selected := savingsScheduleAccount()
	selected.Platform = PlatformAnthropic
	selected.Rental.Platform = PlatformAnthropic
	latest := savingsScheduleAccount()
	latest.Platform = PlatformAnthropic
	latest.Rental.Platform = PlatformAnthropic
	svc := &GatewayService{accountRepo: &refreshAPIAccountRepo{account: latest}}
	groupID := int64(11)
	_, vetoed, _ := svc.GatewayProfitControlVetoLatest(context.Background(), selected, &groupID)
	require.False(t, vetoed)
	groupID = 12
	_, vetoed, _ = svc.GatewayProfitControlVetoLatest(context.Background(), selected, &groupID)
	require.True(t, vetoed, "another group's paid plan cannot grant actual group access")
	groupID = 11
	latest.Credentials["plan_type"] = "free"
	_, vetoed, reason := svc.GatewayProfitControlVetoLatest(context.Background(), selected, &groupID)
	require.True(t, vetoed)
	require.Equal(t, "savings_plan_not_allowed", reason)
}

func TestSavingsSelectionCarriesFallbackGroupWithoutProfitGate(t *testing.T) {
	groupID, fallbackID := int64(10), int64(11)
	account := savingsScheduleAccount()
	account.Platform = PlatformAnthropic
	account.Rental.Platform = PlatformAnthropic
	account.AccountGroups = []AccountGroup{{GroupID: groupID}, {GroupID: fallbackID}}
	repo := &mockAccountRepoForPlatform{accounts: []Account{*account}, accountsByID: map[int64]*Account{account.ID: account}}
	groups := &mockGroupRepoForGateway{groups: map[int64]*Group{
		groupID:    {ID: groupID, Platform: PlatformAnthropic, Status: StatusActive, ClaudeCodeOnly: true, FallbackGroupID: &fallbackID, Hydrated: true},
		fallbackID: {ID: fallbackID, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true},
	}}
	svc := &GatewayService{accountRepo: repo, groupRepo: groups, cfg: testConfig()}
	ctx := context.WithValue(context.Background(), ctxkey.Group, groups.groups[groupID])
	result, err := svc.SelectAccountWithLoadAwareness(ctx, &groupID, "", "", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.ProfitGateActive())
	require.Equal(t, &fallbackID, result.SchedulingGroupID)
	_, vetoed, _ := svc.GatewayProfitControlVetoLatest(ctx, result.Account, result.SchedulingGroupID)
	require.False(t, vetoed)
	_, vetoed, _ = svc.GatewayProfitControlVetoLatest(ctx, result.Account, &groupID)
	require.True(t, vetoed, "entry group has no receiving permission")
}
