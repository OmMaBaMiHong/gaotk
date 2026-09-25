package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type TokenBankService struct {
	Repo     TokenBankRepository
	accounts AccountRepository
	openai   *OpenAIOAuthService
	claude   *OAuthService
	redis    *redis.Client
	client   *http.Client
}

func NewTokenBankService(repo TokenBankRepository, accounts AccountRepository, openai *OpenAIOAuthService, claude *OAuthService, rdb *redis.Client) *TokenBankService {
	return &TokenBankService{Repo: repo, accounts: accounts, openai: openai, claude: claude, redis: rdb, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (s *TokenBankService) policy(ctx context.Context, platform string) (*RentalPolicy, error) {
	policies, err := s.Repo.Policies(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range policies {
		if p.Platform == platform && p.Enabled {
			return &p, nil
		}
	}
	return nil, ErrRentalInvalid
}

func (s *TokenBankService) owned(ctx context.Context, userID, accountID int64) (*Account, error) {
	a, err := s.accounts.GetByID(ctx, accountID)
	if err != nil || a == nil || a.OwnerUserID == nil || *a.OwnerUserID != userID || a.ParentAccountID != nil {
		return nil, ErrRentalNotFound
	}
	return a, nil
}

type RentalImportInput struct {
	Name      string `json:"name"`
	Platform  string `json:"platform"`
	APIKey    string `json:"api_key"`
	AccountID int64  `json:"account_id"`
}

func (s *TokenBankService) Import(ctx context.Context, userID int64, in RentalImportInput) (int64, error) {
	if in.AccountID != 0 {
		return 0, ErrRentalInvalid
	}
	p, err := s.policy(ctx, in.Platform)
	if err != nil {
		return 0, err
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" || len(key) > 8192 {
		return 0, ErrRentalInvalid
	}
	endpoints := map[string]string{PlatformOpenAI: "https://api.openai.com/v1/models", PlatformAnthropic: "https://api.anthropic.com/v1/models", PlatformDeepseek: "https://api.deepseek.com/models"}
	endpoint, ok := endpoints[in.Platform]
	if !ok {
		return 0, ErrRentalInvalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	if in.Platform == PlatformAnthropic {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, ErrRentalInvalid
	}
	defer resp.Body.Close()
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&models) != nil || len(models.Data) == 0 {
		return 0, ErrRentalInvalid
	}
	// A key fingerprint proves duplicate keys, not that distinct keys share a person.
	return s.saveVerified(ctx, userID, in.Name, p, AccountTypeAPIKey, map[string]any{"api_key": key}, rentalIdentity(in.Platform, "key", key), 0)
}

func rentalIdentity(platform, kind, identity string) string {
	sum := sha256.Sum256([]byte(platform + ":" + kind + ":" + identity))
	return hex.EncodeToString(sum[:])
}

type rentalOAuthSession struct {
	UserID    int64  `json:"user_id"`
	AccountID int64  `json:"account_id"`
	Platform  string `json:"platform"`
	Name      string `json:"name"`
	State     string `json:"state"`
}

func (s *TokenBankService) StartOAuth(ctx context.Context, userID int64, in RentalImportInput) (*OpenAIAuthURLResult, error) {
	if _, err := s.policy(ctx, in.Platform); err != nil {
		return nil, err
	}
	if in.AccountID > 0 {
		a, err := s.owned(ctx, userID, in.AccountID)
		if err != nil {
			return nil, err
		}
		if a.Platform != in.Platform || a.Type != AccountTypeOAuth {
			return nil, ErrRentalInvalid
		}
		in.Name = a.Name
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 {
		return nil, ErrRentalInvalid
	}
	var result *OpenAIAuthURLResult
	var err error
	switch in.Platform {
	case PlatformOpenAI:
		result, err = s.openai.GenerateAuthURL(ctx, nil, "", PlatformOpenAI)
	case PlatformAnthropic:
		var r *GenerateAuthURLResult
		r, err = s.claude.GenerateAuthURL(ctx, nil)
		if r != nil {
			result = &OpenAIAuthURLResult{AuthURL: r.AuthURL, SessionID: r.SessionID}
		}
	default:
		return nil, ErrRentalInvalid
	}
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(result.AuthURL)
	if err != nil {
		return nil, err
	}
	state := u.Query().Get("state")
	if state == "" {
		return nil, ErrRentalInvalid
	}
	b, _ := json.Marshal(rentalOAuthSession{UserID: userID, AccountID: in.AccountID, Platform: in.Platform, Name: in.Name, State: state})
	if err := s.redis.Set(ctx, "token-bank:oauth:"+result.SessionID, b, 10*time.Minute).Err(); err != nil {
		return nil, err
	}
	return result, nil
}

type RentalOAuthFinish struct {
	SessionID string `json:"session_id"`
	Code      string `json:"code"`
	State     string `json:"state"`
}

func (s *TokenBankService) FinishOAuth(ctx context.Context, userID int64, in RentalOAuthFinish) (int64, error) {
	// Compare ownership before consuming; another user's request cannot burn it.
	script := redis.NewScript(`local v=redis.call('GET',KEYS[1]); if not v then return '' end; local s=cjson.decode(v); if tostring(s.user_id)~=ARGV[1] or s.state~=ARGV[2] then return '' end; redis.call('DEL',KEYS[1]); return v`)
	data, err := script.Run(ctx, s.redis, []string{"token-bank:oauth:" + in.SessionID}, strconv.FormatInt(userID, 10), in.State).Text()
	if err != nil || data == "" {
		return 0, ErrRentalInvalid
	}
	var session rentalOAuthSession
	if json.Unmarshal([]byte(data), &session) != nil || session.UserID != userID || subtle.ConstantTimeCompare([]byte(session.State), []byte(in.State)) != 1 {
		return 0, ErrRentalInvalid
	}
	p, err := s.policy(ctx, session.Platform)
	if err != nil {
		return 0, err
	}
	var creds map[string]any
	var identity string
	switch session.Platform {
	case PlatformOpenAI:
		token, err := s.openai.ExchangeCode(ctx, &OpenAIExchangeCodeInput{SessionID: in.SessionID, Code: in.Code, State: in.State})
		if err != nil {
			return 0, ErrRentalInvalid
		}
		id := token.ChatGPTAccountID
		if id == "" {
			id = token.ChatGPTUserID
		}
		if id == "" {
			return 0, ErrRentalInvalid
		}
		identity = rentalIdentity(session.Platform, "oauth", id)
		creds = s.openai.BuildAccountCredentials(token)
	case PlatformAnthropic:
		token, err := s.claude.ExchangeCode(ctx, &ExchangeCodeInput{SessionID: in.SessionID, Code: in.Code})
		if err != nil {
			return 0, ErrRentalInvalid
		}
		if token.AccountUUID == "" {
			return 0, ErrRentalInvalid
		}
		identity = rentalIdentity(session.Platform, "oauth", token.AccountUUID)
		creds = map[string]any{"access_token": token.AccessToken, "refresh_token": token.RefreshToken, "expires_at": time.Unix(token.ExpiresAt, 0).Format(time.RFC3339), "scope": token.Scope, "org_uuid": token.OrgUUID, "account_uuid": token.AccountUUID, "email_address": token.EmailAddress}
	default:
		return 0, ErrRentalInvalid
	}
	return s.saveVerified(ctx, userID, session.Name, p, AccountTypeOAuth, creds, identity, session.AccountID)
}

func (s *TokenBankService) saveVerified(ctx context.Context, userID int64, name string, p *RentalPolicy, kind string, creds map[string]any, identity string, accountID int64) (int64, error) {
	name = strings.TrimSpace(name)
	if userID <= 0 || name == "" || len(name) > 100 {
		return 0, ErrRentalInvalid
	}
	if accountID > 0 {
		a, err := s.owned(ctx, userID, accountID)
		if err != nil {
			return 0, err
		}
		if a.RentalIdentity == nil || *a.RentalIdentity != identity || a.Platform != p.Platform {
			return 0, ErrRentalInvalid
		}
		a.Credentials = creds
		a.Status = StatusActive
		a.ErrorMessage = ""
		if err := s.accounts.Update(ctx, a); err != nil {
			return 0, err
		}
		return a.ID, nil
	}
	a := &Account{Name: name, Platform: p.Platform, Type: kind, Credentials: creds, Extra: map[string]any{}, OwnerUserID: &userID, RentalPolicyID: &p.ID, RentalIdentity: &identity, RentalStatus: StatusActive, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 50, AutoPauseOnExpired: true}
	creator, ok := s.accounts.(AccountDuplicateRepository)
	if !ok {
		return 0, errors.New("atomic account creation unavailable")
	}
	if err := creator.CreateWithAccountGroups(ctx, a, []AccountGroup{{GroupID: p.GroupID, Priority: 50}}); err != nil {
		return 0, ErrRentalInvalid
	}
	return a.ID, nil
}
