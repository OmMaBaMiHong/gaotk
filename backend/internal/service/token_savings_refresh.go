package service

import "context"

type tokenSavingsRefreshKey struct{}

// Only a server-refreshed token result can preserve credentials without
// reassigning its owner account to another group during token rotation.
func WithTokenSavingsRefresh(ctx context.Context, info *OpenAITokenInfo) context.Context {
	if info == nil || info.savingsVerifiedPlan == nil {
		return ctx
	}
	return context.WithValue(ctx, tokenSavingsRefreshKey{}, true)
}

func isTokenSavingsRefresh(ctx context.Context) bool {
	trusted, _ := ctx.Value(tokenSavingsRefreshKey{}).(bool)
	return trusted
}

// The ordinary refresh flow preserves non-token credentials. Explicit empty
// values here prevent that merge from retaining an obsolete Pro verification.
func (s *OpenAIOAuthService) reverifySavingsRefresh(ctx context.Context, account *Account, info *OpenAITokenInfo) {
	credentials := MergeCredentials(account.Credentials, s.BuildAccountCredentials(info))
	verified, err := s.VerifySavingsAccount(ctx, account.Platform, account.Type, credentials)
	plan := ""
	if err == nil {
		plan, _ = verified["savings_verified_plan_type"].(string)
		info.savingsVerifiedAt, _ = verified["savings_verified_at"].(int64)
	}
	info.PlanType = plan
	info.savingsVerifiedPlan = &plan
}

func (s *OAuthService) reverifyClaudeSavingsRefresh(ctx context.Context, account *Account, info *TokenInfo) {
	credentials := MergeCredentials(account.Credentials, BuildClaudeAccountCredentials(info))
	verified, err := s.VerifySavingsAccount(ctx, account.Platform, account.Type, credentials)
	plan := ""
	if err == nil {
		plan, _ = verified["savings_verified_plan_type"].(string)
		info.savingsVerifiedAt, _ = verified["savings_verified_at"].(int64)
	}
	info.savingsVerifiedPlan = &plan
}
func WithClaudeSavingsRefresh(ctx context.Context, info *TokenInfo) context.Context {
	if info == nil || info.savingsVerifiedPlan == nil {
		return ctx
	}
	return context.WithValue(ctx, tokenSavingsRefreshKey{}, true)
}
