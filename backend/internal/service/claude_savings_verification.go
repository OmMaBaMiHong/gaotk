package service

import (
	"context"
	"strings"
	"time"
)

// ClaudeSubscriptionProfile describes the identity and selected organization
// returned by Anthropic's authenticated OAuth profile endpoint.
type ClaudeSubscriptionProfile struct {
	Account struct {
		UUID  string `json:"uuid"`
		Email string `json:"email"`
	} `json:"account"`
	Organization struct {
		UUID             string `json:"uuid"`
		OrganizationType string `json:"organization_type"`
	} `json:"organization"`
}

// Keep subscription verification separate from the existing OAuth flow port.
type ClaudeSubscriptionProfileClient interface {
	FetchSubscriptionProfile(context.Context, string, string) (*ClaudeSubscriptionProfile, error)
}

func (s *OAuthService) VerifySavingsAccount(ctx context.Context, platform, accountType string, credentials map[string]any) (map[string]any, error) {
	if s == nil || platform != PlatformAnthropic || accountType != AccountTypeOAuth {
		return nil, savingsPlanUnverified()
	}
	profileClient, ok := s.oauthClient.(ClaudeSubscriptionProfileClient)
	if !ok {
		return nil, savingsPlanUnverified()
	}
	verified := make(map[string]any, len(credentials))
	for key, value := range credentials {
		verified[key] = value
	}
	for _, key := range []string{"savings_verified_plan_type", "savings_verified_at", "plan_type", "subscription_type", "subscription_tier", "tier_id", "entitlement_status"} {
		delete(verified, key)
	}
	account := &Account{Credentials: verified}
	accessToken := strings.TrimSpace(account.GetCredential("access_token"))
	if accessToken == "" {
		refreshToken := strings.TrimSpace(account.GetCredential("refresh_token"))
		if refreshToken == "" {
			return nil, savingsPlanUnverified()
		}
		info, err := s.RefreshToken(ctx, refreshToken, "")
		if err != nil || info == nil || strings.TrimSpace(info.AccessToken) == "" {
			return nil, savingsPlanUnverified()
		}
		for key, value := range BuildClaudeAccountCredentials(info) {
			verified[key] = value
		}
		accessToken = strings.TrimSpace(info.AccessToken)
	}
	profile, err := profileClient.FetchSubscriptionProfile(ctx, accessToken, "")
	if err != nil || profile == nil || strings.TrimSpace(profile.Account.UUID) == "" || strings.TrimSpace(profile.Account.Email) == "" || strings.TrimSpace(profile.Organization.UUID) == "" {
		return nil, savingsPlanUnverified()
	}
	// The organization associated with this token determines its personal plan.
	// Account-wide subscription flags and rate-limit tiers can describe other plans.
	var plan string
	switch profile.Organization.OrganizationType {
	case "claude_pro":
		plan = "pro"
	case "claude_max":
		plan = "max"
	default:
		return nil, savingsPlanUnverified()
	}
	for _, key := range []string{"org_uuid", "organization_uuid", "organization_id"} {
		if value := strings.TrimSpace(account.GetCredential(key)); value != "" && value != profile.Organization.UUID {
			return nil, savingsPlanUnverified()
		}
	}
	for _, key := range []string{"account_uuid", "account_id"} {
		if value := strings.TrimSpace(account.GetCredential(key)); value != "" && value != profile.Account.UUID {
			return nil, savingsPlanUnverified()
		}
	}
	verified["access_token"] = accessToken
	verified["account_uuid"] = profile.Account.UUID
	verified["org_uuid"] = profile.Organization.UUID
	verified["email_address"] = profile.Account.Email
	// Replace aliases too: no stale owner-submitted identity should survive.
	for _, key := range []string{"account_id", "organization_uuid", "organization_id", "email"} {
		delete(verified, key)
	}
	verified["plan_type"] = plan
	verified["subscription_type"] = plan
	verified["savings_verified_plan_type"] = plan
	verified["savings_verified_at"] = time.Now().Unix()
	return verified, nil
}
