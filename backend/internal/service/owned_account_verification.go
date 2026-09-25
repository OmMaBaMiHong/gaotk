package service

import (
	"context"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// SavingsAccountVerifier returns provider-verified credentials. Caller supplied
// subscription labels and decoded JWT claims never establish group eligibility.
type SavingsAccountVerifier interface {
	VerifySavingsAccount(context.Context, string, string, map[string]any) (map[string]any, error)
}

func NormalizeSavingsPlan(plan string) string {
	plan = strings.Map(func(r rune) rune {
		if r == ' ' || r == '_' || r == '-' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(plan)))
	if plan == "chatgptpro" {
		return "pro"
	}
	return plan
}

func savingsPlanUnverified() error {
	return infraerrors.BadRequest("SAVINGS_PLAN_UNVERIFIED", "无法核验该账号的真实套餐，请重新授权后重试")
}

func (s *OpenAIOAuthService) VerifySavingsAccount(ctx context.Context, platform, accountType string, credentials map[string]any) (map[string]any, error) {
	verified := make(map[string]any, len(credentials))
	for key, value := range credentials {
		verified[key] = value
	}
	delete(verified, "savings_verified_plan_type")
	delete(verified, "savings_verified_at")
	if platform != PlatformOpenAI {
		return verified, nil
	}
	delete(verified, "plan_type")
	// Alternative runtime identities must never override the token just verified.
	for _, key := range []string{"auth_mode", "agent_identity", "runtime_id", "agent_runtime_id", "agent_private_key", "auth_tokens"} {
		delete(verified, key)
	}
	if accountType != AccountTypeOAuth {
		return verified, nil
	}
	if s == nil {
		return nil, savingsPlanUnverified()
	}
	account := &Account{Platform: platform, Type: accountType, Credentials: verified}
	var info *OpenAITokenInfo
	var err error
	accessToken := strings.TrimSpace(account.GetCredential("access_token"))
	switch {
	case account.IsOpenAIPersonalAccessToken() || strings.HasPrefix(accessToken, "at-"):
		info, err = s.ValidateCodexPersonalAccessToken(ctx, accessToken, "")
	case accessToken != "":
		info, err = s.verifySavingsUsage(ctx, account)
	case strings.TrimSpace(account.GetCredential("refresh_token")) != "":
		// Only the token returned by this server-to-provider exchange is trusted.
		// Persist its rotated access/refresh token together with the verified plan.
		if s.oauthClient == nil {
			return nil, savingsPlanUnverified()
		}
		info, err = s.refreshSavingsToken(ctx, account)
	default:
		return nil, savingsPlanUnverified()
	}
	if err != nil || info == nil {
		return nil, savingsPlanUnverified()
	}
	plan := NormalizeSavingsPlan(info.PlanType)
	if !IsTokenSavingsPlanForPlatform(PlatformOpenAI, plan) {
		return nil, savingsPlanUnverified()
	}
	for key, value := range s.BuildAccountCredentials(info) {
		verified[key] = value
	}
	verified = NormalizeOpenAIPersonalAccessTokenCredentials(nil, info, verified)
	verified["plan_type"] = plan
	verified["savings_verified_plan_type"] = plan
	verified["savings_verified_at"] = time.Now().Unix()
	return verified, nil
}

func (s *OpenAIOAuthService) refreshSavingsToken(ctx context.Context, account *Account) (*OpenAITokenInfo, error) {
	// Reuse the original OAuth transport, but not best-effort workspace
	// enrichment: a workspace plan is not proof of this token's personal plan.
	response, err := s.oauthClient.RefreshTokenWithClientID(ctx, account.GetCredential("refresh_token"), "", account.GetCredential("client_id"))
	if err != nil || response == nil || strings.TrimSpace(response.AccessToken) == "" {
		return nil, savingsPlanUnverified()
	}
	info := &OpenAITokenInfo{AccessToken: response.AccessToken, RefreshToken: response.RefreshToken, IDToken: response.IDToken, ClientID: account.GetCredential("client_id"), ExpiresAt: time.Now().Unix() + int64(response.ExpiresIn)}
	// Parsing is safe only here because this JWT came directly from the
	// authenticated upstream response, never from submitted JSON credentials.
	if claims, parseErr := openai.ParseIDToken(response.IDToken); parseErr == nil {
		user := claims.GetUserInfo()
		info.PlanType, info.ChatGPTAccountID, info.ChatGPTUserID, info.Email, info.OrganizationID = user.PlanType, user.ChatGPTAccountID, user.ChatGPTUserID, user.Email, user.OrganizationID
	}
	if info.PlanType == "" || info.ChatGPTAccountID == "" {
		fresh := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": response.AccessToken}}
		usage, err := s.verifySavingsUsage(ctx, fresh)
		if err != nil {
			return nil, err
		}
		info.PlanType, info.ChatGPTAccountID, info.ChatGPTUserID, info.Email = usage.PlanType, usage.ChatGPTAccountID, usage.ChatGPTUserID, usage.Email
	}
	return info, nil
}

// verifySavingsUsage is deliberately strict, unlike display-only best-effort
// enrichment. The fixed upstream authenticates both the token and selected
// account; the returned account ID must describe that same credential target.
func (s *OpenAIOAuthService) verifySavingsUsage(ctx context.Context, account *Account) (*OpenAITokenInfo, error) {
	if s.privacyClientFactory == nil {
		return nil, savingsPlanUnverified()
	}
	client, err := s.privacyClientFactory("")
	if err != nil {
		return nil, savingsPlanUnverified()
	}
	ctx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()
	accountID := strings.TrimSpace(account.GetCredential("chatgpt_account_id"))
	if accountID == "" {
		accountID = strings.TrimSpace(account.GetCredential("organization_id"))
	}
	headers := buildCodexCommonHeaders(account.GetCredential("access_token"), accountID, account.IsChatGPTAccountFedRAMP())
	var usage OpenAIQuotaUsage
	request := client.R().SetContext(ctx).SetHeaders(headers).SetSuccessResult(&usage)
	resp, err := request.Get(chatGPTUsageURL)
	if err != nil || !resp.IsSuccessState() || strings.TrimSpace(usage.AccountID) == "" || (accountID != "" && accountID != usage.AccountID) {
		return nil, savingsPlanUnverified()
	}
	return &OpenAITokenInfo{AccessToken: account.GetCredential("access_token"), ChatGPTAccountID: usage.AccountID, ChatGPTUserID: usage.UserID, Email: usage.Email, PlanType: usage.PlanType}, nil
}

// SavingsAccountVerifiers dispatches into the existing provider OAuth services.
type SavingsAccountVerifiers struct {
	OpenAI *OpenAIOAuthService
	Claude *OAuthService
}

func (v SavingsAccountVerifiers) VerifySavingsAccount(ctx context.Context, platform, accountType string, credentials map[string]any) (map[string]any, error) {
	if err := validateSavingsAccountType(platform, accountType); err != nil {
		return nil, err
	}
	if accountType == AccountTypeAPIKey {
		clean := make(map[string]any, len(credentials))
		for k, value := range credentials {
			clean[k] = value
		}
		for _, k := range []string{"savings_verified_plan_type", "savings_verified_at", "plan_type", "subscription_type"} {
			delete(clean, k)
		}
		return clean, nil
	}
	switch platform {
	case PlatformOpenAI:
		return v.OpenAI.VerifySavingsAccount(ctx, platform, accountType, credentials)
	case PlatformAnthropic:
		return v.Claude.VerifySavingsAccount(ctx, platform, accountType, credentials)
	}
	return nil, savingsPlanUnverified()
}
