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

func (s ownedGroupsStub) GetSavingsReceivingGroups(context.Context, string) ([]int64, error) {
	return s.groups, s.err
}

func TestOwnedAccountCreateUsesServerOwnerAndAllReceivingGroups(t *testing.T) {
	attacker := int64(900)
	proxy := int64(10)
	multiplier := 0.01
	load := 999
	input := &CreateAccountInput{OwnerUserID: &attacker, Platform: PlatformOpenAI, GroupIDs: []int64{999}, ProxyID: &proxy, Priority: 1, Concurrency: 1000, RateMultiplier: &multiplier, LoadFactor: &load, SkipMixedChannelCheck: true, Extra: map[string]any{"quota_used": 0}, Credentials: map[string]any{"access_token": "mine", "base_url": "https://attacker.invalid", "model_mapping": map[string]any{"a": "b"}}}
	ctx := WithOwnedAccountScope(context.Background(), 42, ownedGroupsStub{groups: []int64{7, 8}})
	require.NoError(t, prepareOwnedAccountCreate(ctx, input))
	require.Equal(t, int64(42), *input.OwnerUserID)
	require.Equal(t, []int64{7, 8}, input.GroupIDs)
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
