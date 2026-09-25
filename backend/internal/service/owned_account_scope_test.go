package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type ownedGroupsStub struct {
	groups []int64
	err    error
}

func (s ownedGroupsStub) GetSavingsReceivingGroups(context.Context, string, string, string) ([]int64, error) {
	return s.groups, s.err
}

func TestOwnedAccountCreateUsesServerOwnerAndSelectedReceivingGroup(t *testing.T) {
	attacker := int64(900)
	proxy := int64(10)
	multiplier := 0.01
	load := 999
	input := &CreateAccountInput{OwnerUserID: &attacker, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, GroupIDs: []int64{999}, ProxyID: &proxy, Priority: 1, Concurrency: 1000, RateMultiplier: &multiplier, LoadFactor: &load, SkipMixedChannelCheck: true, Extra: map[string]any{"quota_used": 0}, Credentials: map[string]any{"access_token": "mine", "base_url": "https://attacker.invalid", "model_mapping": map[string]any{"a": "b"}}}
	ctx := WithOwnedAccountScope(context.Background(), 42, ownedGroupsStub{groups: []int64{7}})
	require.NoError(t, prepareOwnedAccountCreate(ctx, input))
	require.Equal(t, int64(42), *input.OwnerUserID)
	require.Equal(t, []int64{7}, input.GroupIDs)
	require.Nil(t, input.ProxyID)
	require.Nil(t, input.RateMultiplier)
	require.Nil(t, input.LoadFactor)
	require.Nil(t, input.Extra)
	require.Equal(t, 3, input.Concurrency)
	require.Equal(t, 50, input.Priority)
	require.False(t, input.SkipMixedChannelCheck)
	require.Equal(t, map[string]any{"access_token": "mine"}, input.Credentials)
	account, err := buildAccountForCreate(input, nil)
	require.NoError(t, err)
	require.Equal(t, int64(42), *account.OwnerUserID)
}
func TestOwnedAccountCreateFailsWithoutReceivingGroups(t *testing.T) {
	ctx := WithOwnedAccountScope(context.Background(), 42, ownedGroupsStub{})
	require.Error(t, prepareOwnedAccountCreate(ctx, &CreateAccountInput{Platform: PlatformOpenAI}))
}
func TestOwnedAccountScopeDoesNotChangeAdminInputs(t *testing.T) {
	input := &CreateAccountInput{GroupIDs: []int64{11, 22}, Priority: 9, Credentials: map[string]any{"base_url": "https://admin.invalid"}}
	require.NoError(t, prepareOwnedAccountCreate(context.Background(), input))
	require.Equal(t, []int64{11, 22}, input.GroupIDs)
	require.Equal(t, 9, input.Priority)
	require.Contains(t, input.Credentials, "base_url")
}
func TestOwnedAccountUpdateCannotMoveGroupsOrOverrideAdminCredentials(t *testing.T) {
	owner := int64(42)
	groups := []int64{100}
	priority := 1
	account := &Account{OwnerUserID: &owner, Credentials: map[string]any{"access_token": "old", "base_url": "https://admin.invalid", "model_mapping": map[string]any{"a": "b"}}}
	input := &UpdateAccountInput{GroupIDs: &groups, Priority: &priority, Extra: map[string]any{"quota_used": 0}, Credentials: map[string]any{"access_token": "new", "base_url": "https://evil.invalid"}}
	ctx := WithOwnedAccountScope(context.Background(), owner, nil)
	require.NoError(t, prepareOwnedAccountUpdate(ctx, account, input))
	require.Nil(t, input.GroupIDs)
	require.Nil(t, input.Priority)
	require.Nil(t, input.Extra)
	require.Equal(t, "new", input.Credentials["access_token"])
	require.Equal(t, "https://admin.invalid", input.Credentials["base_url"])
	require.ErrorIs(t, prepareOwnedAccountUpdate(WithOwnedAccountScope(context.Background(), 43, nil), account, input), ErrAccountNotFound)
}
func TestOwnedOAuthSessionsRejectOtherUsersAndAdminSessions(t *testing.T) {
	ctx := WithOwnedAccountScope(context.Background(), 42, nil)
	require.NoError(t, checkOwnedOAuthSession(ctx, 42))
	require.Error(t, checkOwnedOAuthSession(ctx, 43))
	require.Error(t, checkOwnedOAuthSession(ctx, 0))
	require.NoError(t, checkOwnedOAuthSession(context.Background(), 0))
}

func TestOwnedAccountImportsCannotCreateCustomUpstream(t *testing.T) {
	ctx := WithOwnedAccountScope(context.Background(), 42, ownedGroupsStub{groups: []int64{7}})
	input := &CreateAccountInput{Platform: PlatformAntigravity, Type: AccountTypeUpstream, Credentials: map[string]any{"base_url": "https://relay.invalid", "api_key": "secret"}}
	require.ErrorContains(t, prepareOwnedAccountCreate(ctx, input), "Custom upstream connections")
	require.NoError(t, prepareOwnedAccountCreate(context.Background(), input), "admin upstream flow stays unchanged")
}
func TestOwnedAuthenticationFieldsArePreserved(t *testing.T) {
	fields := map[string]any{"auth_mode": "agent_identity", "agent_private_key": "private", "agent_runtime_id": "runtime", "task_id": "task", "chatgpt_account_is_fedramp": true, "api_protocol": "anthropic", "zhipu_organization": "org", "zhipu_project": "project", "client_email": "service@example.com", "location": "us-central1", "service_account_json": "json", "aws_access_key_id": "aws", "aws_secret_access_key": "secret", "sub": "xai-sub", "team_id": "team"}
	require.Equal(t, fields, OwnedAccountCredentials(fields))
	require.Equal(t, map[string]any{"account_uuid": "claude-identity"}, OwnedAccountExtra(map[string]any{"account_uuid": "claude-identity", "quota_used": 0, "privacy_mode": "set"}))
}

func TestOwnedOpenAICreateRejectsUnverifiedClientPlan(t *testing.T) {
	ctx := WithOwnedAccountScope(context.Background(), 42, ownedGroupsStub{groups: []int64{7}})
	input := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "unverified", "plan_type": "pro"}}
	require.Error(t, prepareOwnedAccountCreate(ctx, input), "an owner-supplied Pro label cannot authorize a receiving group without upstream verification")
}

type ownedVerifierFunc func(context.Context, string, string, map[string]any) (map[string]any, error)

func (f ownedVerifierFunc) VerifySavingsAccount(ctx context.Context, platform, accountType string, creds map[string]any) (map[string]any, error) {
	return f(ctx, platform, accountType, creds)
}

type ownedPlanGroupsFunc func(context.Context, string, string, string) ([]int64, error)

func (f ownedPlanGroupsFunc) GetSavingsReceivingGroups(ctx context.Context, platform, accountType, plan string) ([]int64, error) {
	return f(ctx, platform, accountType, plan)
}

func TestOwnedOpenAICreateUsesOnlyVerifiedPlanForGroupSelection(t *testing.T) {
	verifier := ownedVerifierFunc(func(_ context.Context, _, _ string, creds map[string]any) (map[string]any, error) {
		require.NotContains(t, creds, "savings_verified_plan_type")
		require.NotContains(t, creds, "savings_verified_at")
		creds["plan_type"] = "plus"
		creds["savings_verified_plan_type"] = "plus"
		creds["savings_verified_at"] = int64(123)
		return creds, nil
	})
	groups := ownedPlanGroupsFunc(func(_ context.Context, platform, accountType, plan string) ([]int64, error) {
		require.Equal(t, PlatformOpenAI, platform)
		require.Equal(t, "plus", plan)
		return []int64{8}, nil
	})
	ctx := WithOwnedAccountScope(context.Background(), 42, groups, verifier)
	input := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "access", "plan_type": "pro", "savings_verified_plan_type": "pro", "savings_verified_at": 999}}
	require.NoError(t, prepareOwnedAccountCreate(ctx, input))
	require.Equal(t, []int64{8}, input.GroupIDs)
	require.Equal(t, "plus", input.Credentials["savings_verified_plan_type"])
}

func TestOwnedOpenAIReauthorizationReassignsGroupAndReplacesIdentity(t *testing.T) {
	owner := int64(42)
	account := &Account{OwnerUserID: &owner, Platform: PlatformOpenAI, Type: AccountTypeOAuth, GroupIDs: []int64{7}, Credentials: map[string]any{"access_token": "old", "refresh_token": "old-refresh", "chatgpt_account_id": "old-id", "openai_auth_mode": "personal_access_token", "plan_type": "pro", "savings_verified_plan_type": "pro", "model_mapping": map[string]any{"model": "upstream"}}}
	verifier := ownedVerifierFunc(func(_ context.Context, _, _ string, creds map[string]any) (map[string]any, error) {
		require.Equal(t, "new", creds["access_token"])
		require.NotContains(t, creds, "refresh_token")
		require.NotContains(t, creds, "chatgpt_account_id")
		require.NotContains(t, creds, "openai_auth_mode", "a previous PAT mode must not force a newly authorized OAuth token through PAT validation")
		require.Contains(t, creds, "model_mapping")
		creds["plan_type"] = "plus"
		creds["savings_verified_plan_type"] = "plus"
		return creds, nil
	})
	groups := ownedPlanGroupsFunc(func(_ context.Context, _, accountType, plan string) ([]int64, error) {
		require.Equal(t, "plus", plan)
		return []int64{8}, nil
	})
	input := &UpdateAccountInput{Credentials: map[string]any{"access_token": "new", "plan_type": "pro"}}
	require.NoError(t, prepareOwnedAccountUpdate(WithOwnedAccountScope(context.Background(), owner, groups, verifier), account, input))
	require.Equal(t, []int64{8}, *input.GroupIDs)
	require.Equal(t, "plus", input.Credentials["savings_verified_plan_type"])
	require.Equal(t, "old-refresh", account.Credentials["refresh_token"], "preparation must not mutate the persisted account on a failed later save")
	require.Equal(t, []int64{7}, account.GroupIDs)
}

func TestOwnedOpenAIReauthorizationFailsWithoutEligibleGroup(t *testing.T) {
	owner := int64(42)
	account := &Account{OwnerUserID: &owner, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "old", "plan_type": "pro"}}
	verifier := ownedVerifierFunc(func(_ context.Context, _, _ string, creds map[string]any) (map[string]any, error) {
		creds["savings_verified_plan_type"] = "free"
		return creds, nil
	})
	input := &UpdateAccountInput{Credentials: map[string]any{"access_token": "new"}}
	require.Error(t, prepareOwnedAccountUpdate(WithOwnedAccountScope(context.Background(), owner, ownedGroupsStub{}, verifier), account, input))
	require.Equal(t, "old", account.Credentials["access_token"])
}

func TestOwnedOpenAIRefreshPreservesRotationAfterVerificationFailure(t *testing.T) {
	owner := int64(42)
	account := &Account{OwnerUserID: &owner, Platform: PlatformOpenAI, Type: AccountTypeOAuth, GroupIDs: []int64{7}, Credentials: map[string]any{"refresh_token": "old"}}
	empty := ""
	ctx := WithTokenSavingsRefresh(WithOwnedAccountScope(context.Background(), owner, nil), &OpenAITokenInfo{savingsVerifiedPlan: &empty})
	groups := []int64{99}
	input := &UpdateAccountInput{GroupIDs: &groups, Credentials: map[string]any{"refresh_token": "rotated", "access_token": "rotated-access", "savings_verified_plan_type": "", "savings_verified_at": int64(0)}}
	require.NoError(t, prepareOwnedAccountUpdate(ctx, account, input))
	require.Nil(t, input.GroupIDs, "refresh keeps the existing binding while the runtime eligibility guard blocks invalid plans")
	require.Equal(t, "rotated", input.Credentials["refresh_token"])
	require.Contains(t, input.Credentials, "savings_verified_plan_type")
	require.Empty(t, input.Credentials["savings_verified_plan_type"])
	wrongOwner := WithTokenSavingsRefresh(WithOwnedAccountScope(context.Background(), 43, nil), &OpenAITokenInfo{savingsVerifiedPlan: &empty})
	require.ErrorIs(t, prepareOwnedAccountUpdate(wrongOwner, account, input), ErrAccountNotFound)
	untrusted := WithTokenSavingsRefresh(WithOwnedAccountScope(context.Background(), owner, nil), &OpenAITokenInfo{PlanType: "pro"})
	require.Error(t, prepareOwnedAccountUpdate(untrusted, account, input), "ordinary token labels cannot create a trusted refresh context")
}
