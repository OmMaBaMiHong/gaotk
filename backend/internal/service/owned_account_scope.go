package service

import (
	"context"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type ownedAccountScopeKey struct{}
type SavingsReceivingGroups interface {
	GetSavingsReceivingGroups(context.Context, string) ([]int64, error)
}
type ownedAccountScope struct {
	userID int64
	groups SavingsReceivingGroups
}

// WithOwnedAccountScope is used only after JWT authentication on the explicit user account routes.
func WithOwnedAccountScope(ctx context.Context, userID int64, groups SavingsReceivingGroups) context.Context {
	return context.WithValue(ctx, ownedAccountScopeKey{}, ownedAccountScope{userID, groups})
}
func OwnedAccountUserID(ctx context.Context) int64 {
	scope, _ := ctx.Value(ownedAccountScopeKey{}).(ownedAccountScope)
	return scope.userID
}
func CheckOwnedAccount(ctx context.Context, account *Account) error {
	if id := OwnedAccountUserID(ctx); id > 0 && (account == nil || account.OwnerUserID == nil || *account.OwnerUserID != id) {
		return ErrAccountNotFound
	}
	return nil
}
func checkOwnedOAuthSession(ctx context.Context, ownerID int64) error {
	if id := OwnedAccountUserID(ctx); id > 0 && ownerID != id {
		return infraerrors.NotFound("OAUTH_SESSION_NOT_FOUND", "Authorization session not found or expired")
	}
	return nil
}

// Only provider authentication/identity fields can be supplied by account owners.
// Routing, model mappings, billing, quota overrides and scheduling remain server managed.
func OwnedAccountCredentials(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"access_token", "refresh_token", "id_token", "api_key", "session_token", "session_key", "sso_token", "expires_at", "token_type", "scope", "email", "account_id", "user_id", "chatgpt_account_id", "chatgpt_user_id", "organization_id", "org_uuid", "organization_uuid", "subscription_type", "plan_type", "client_id", "project_id", "oauth_type", "tier_id", "service_account", "service_account_json", "credentials_json", "aws_access_key_id", "aws_secret_access_key", "aws_session_token", "aws_region", "region", "auth_type", "account_mode", "refresh_expires_at", "access_token_expires_at", "token_expires_at", "auth_tokens", "agent_identity", "runtime_id", "device_id", "auth_mode", "agent_runtime_id", "agent_private_key", "chatgpt_account_is_fedramp", "sub", "team_id", "subscription_tier", "entitlement_status", "account_uuid", "email_address", "client_email", "location", "api_protocol", "zhipu_organization", "zhipu_project", "task_id", "subscription_expires_at"} {
		if value, ok := input[key]; ok {
			out[key] = value
		}
	}
	return out
}
func prepareOwnedAccountCreate(ctx context.Context, input *CreateAccountInput) error {
	scope, ok := ctx.Value(ownedAccountScopeKey{}).(ownedAccountScope)
	if !ok {
		return nil
	}
	if scope.userID <= 0 || scope.groups == nil {
		return infraerrors.Forbidden("ACCOUNT_OWNER_REQUIRED", "Account owner is required")
	}
	if input.Type == AccountTypeUpstream {
		return infraerrors.BadRequest("OWNED_ACCOUNT_UPSTREAM_NOT_SUPPORTED", "Custom upstream connections must be configured by an administrator")
	}
	groups, err := scope.groups.GetSavingsReceivingGroups(ctx, input.Platform)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return infraerrors.BadRequest("SAVINGS_RECEIVING_GROUP_NOT_CONFIGURED", "This platform has no receiving groups configured")
	}
	input.OwnerUserID = &scope.userID
	input.GroupIDs = groups
	input.ProxyID = nil
	input.Priority = 50
	input.Concurrency = 3
	input.RateMultiplier = nil
	input.LoadFactor = nil
	input.ProbeEnabled = nil
	input.Extra = OwnedAccountExtra(input.Extra)
	input.Credentials = OwnedAccountCredentials(input.Credentials)
	input.SkipDefaultGroupBind = true
	input.SkipMixedChannelCheck = false
	return nil
}
func prepareOwnedAccountUpdate(ctx context.Context, account *Account, input *UpdateAccountInput) error {
	if OwnedAccountUserID(ctx) == 0 {
		return nil
	}
	if err := CheckOwnedAccount(ctx, account); err != nil {
		return err
	}
	if input.Type == AccountTypeUpstream {
		return infraerrors.BadRequest("OWNED_ACCOUNT_UPSTREAM_NOT_SUPPORTED", "Custom upstream connections must be configured by an administrator")
	}
	input.GroupIDs = nil
	input.ProxyID = nil
	input.Priority = nil
	input.Concurrency = nil
	input.RateMultiplier = nil
	input.LoadFactor = nil
	input.ProbeEnabled = nil
	input.RateSyncEnabled = nil
	input.Extra = nil
	if input.Credentials != nil {
		// Preserve administrator routing while replacing only owner supplied credentials.
		credentials := make(map[string]any, len(account.Credentials))
		for key, value := range account.Credentials {
			credentials[key] = value
		}
		for key, value := range OwnedAccountCredentials(input.Credentials) {
			credentials[key] = value
		}
		input.Credentials = credentials
	}
	return nil
}

// Claude request identity is stored in Extra by the original authorization flow.
// Preserve that identity without accepting quota, routing or scheduling configuration.
func OwnedAccountExtra(input map[string]any) map[string]any {
	var out map[string]any
	for _, key := range []string{"org_uuid", "account_uuid", "email_address", "email", "name"} {
		if value, ok := input[key]; ok {
			if out == nil {
				out = make(map[string]any)
			}
			out[key] = value
		}
	}
	return out
}

type accountListOwnerFilterKey struct{}

// WithAccountListOwnerFilter narrows an administrator's list without changing its permissions.
func WithAccountListOwnerFilter(ctx context.Context, ownerID int64) context.Context {
	return context.WithValue(ctx, accountListOwnerFilterKey{}, ownerID)
}
func AccountListOwnerID(ctx context.Context) int64 {
	if ownerID := OwnedAccountUserID(ctx); ownerID > 0 {
		return ownerID
	}
	ownerID, _ := ctx.Value(accountListOwnerFilterKey{}).(int64)
	return ownerID
}
