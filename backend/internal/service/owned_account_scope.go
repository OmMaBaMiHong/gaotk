package service

import (
	"context"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type ownedAccountScopeKey struct{}
type SavingsReceivingGroups interface {
	GetSavingsReceivingGroups(context.Context, string, string, string) ([]int64, error)
}
type ownedAccountScope struct {
	userID   int64
	groups   SavingsReceivingGroups
	verifier SavingsAccountVerifier
}

// WithOwnedAccountScope is used only after JWT authentication on the explicit user account routes.
func WithOwnedAccountScope(ctx context.Context, userID int64, groups SavingsReceivingGroups, verifiers ...SavingsAccountVerifier) context.Context {
	scope := ownedAccountScope{userID: userID, groups: groups}
	if len(verifiers) > 0 {
		scope.verifier = verifiers[0]
	}
	return context.WithValue(ctx, ownedAccountScopeKey{}, scope)
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
	if err := validateSavingsAccountType(input.Platform, input.Type); err != nil {
		return err
	}
	credentials := OwnedAccountCredentials(input.Credentials)
	if input.Type == AccountTypeOAuth {
		if scope.verifier == nil {
			return savingsPlanUnverified()
		}
		var err error
		credentials, err = scope.verifier.VerifySavingsAccount(ctx, input.Platform, input.Type, credentials)
		if err != nil {
			return err
		}
	}
	plan, _ := credentials["savings_verified_plan_type"].(string)
	groups, err := scope.groups.GetSavingsReceivingGroups(ctx, input.Platform, input.Type, plan)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return infraerrors.BadRequest("SAVINGS_RECEIVING_GROUP_NOT_CONFIGURED", "该平台或套餐暂无符合条件的接收分组")
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
	if input.Platform == PlatformAnthropic && input.Type == AccountTypeOAuth {
		input.Extra = savingsClaudeExtra(input.Extra, credentials)
	}
	input.Credentials = credentials
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
	// Upstream refresh already verified (or explicitly invalidated) the plan.
	// Keep rotated credentials even when the old group no longer admits it;
	// the runtime gate blocks that group using the new/empty verification.
	if (account.Platform == PlatformOpenAI || account.Platform == PlatformAnthropic) && isTokenSavingsRefresh(ctx) {
		return nil
	}
	if input.Credentials != nil {
		// Preserve administrator routing while replacing only owner supplied credentials.
		credentials := make(map[string]any, len(account.Credentials))
		for key, value := range account.Credentials {
			credentials[key] = value
		}
		supplied := OwnedAccountCredentials(input.Credentials)
		// Re-authorization must not retain the old identity's refresh token
		// when only a new access token is supplied, or vice versa.
		if account.Platform == PlatformOpenAI || account.Platform == PlatformAnthropic {
			for _, key := range []string{"access_token", "refresh_token", "api_key"} {
				value, exists := supplied[key]
				text, _ := value.(string)
				if exists && text != account.GetCredential(key) {
					for authKey := range OwnedAccountCredentials(account.Credentials) {
						delete(credentials, authKey)
					}
					delete(credentials, openAIAuthModeLegacyCredentialKey)
					break
				}
			}
		}
		for key, value := range supplied {
			credentials[key] = value
		}
		input.Credentials = credentials
	}
	if ((account.Platform == PlatformOpenAI || account.Platform == PlatformAnthropic) && input.Credentials != nil) || (input.Type != "" && input.Type != account.Type) {
		scope, _ := ctx.Value(ownedAccountScopeKey{}).(ownedAccountScope)
		if scope.verifier == nil || scope.groups == nil {
			return savingsPlanUnverified()
		}
		credentials := input.Credentials
		if credentials == nil {
			credentials = account.Credentials
		}
		accountType := account.Type
		if input.Type != "" {
			accountType = input.Type
		}
		if err := validateSavingsAccountType(account.Platform, accountType); err != nil {
			return err
		}
		verified, err := scope.verifier.VerifySavingsAccount(ctx, account.Platform, accountType, credentials)
		if err != nil {
			return err
		}
		plan, _ := verified["savings_verified_plan_type"].(string)
		groups, err := scope.groups.GetSavingsReceivingGroups(ctx, account.Platform, accountType, plan)
		if err != nil {
			return err
		}
		if len(groups) == 0 {
			return infraerrors.BadRequest("SAVINGS_RECEIVING_GROUP_NOT_CONFIGURED", "该平台或套餐暂无符合条件的接收分组")
		}
		input.Credentials = verified
		input.GroupIDs = &groups
		if account.Platform == PlatformAnthropic && accountType == AccountTypeOAuth {
			input.Extra = savingsClaudeExtra(account.Extra, verified)
		}
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

func validateSavingsAccountType(platform, accountType string) error {
	if accountType == AccountTypeAPIKey {
		return nil
	}
	if accountType == AccountTypeOAuth && (platform == PlatformOpenAI || platform == PlatformAnthropic) {
		return nil
	}
	return infraerrors.BadRequest("SAVINGS_ACCOUNT_TYPE_UNSUPPORTED", "当前储蓄接收规则不支持该账号授权类型")
}

// The Claude gateway uses Extra identity; only the provider-verified identity
// may populate it, while existing administrator options remain intact on edit.
func savingsClaudeExtra(existing, credentials map[string]any) map[string]any {
	extra := make(map[string]any, len(existing)+3)
	for k, v := range existing {
		extra[k] = v
	}
	for _, key := range []string{"account_uuid", "org_uuid", "email_address"} {
		extra[key] = credentials[key]
	}
	delete(extra, "email")
	delete(extra, "name")
	return extra
}
