package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func (s *SettingService) SetOAuthClientAppRepository(repo OAuthClientAppRepository) {
	s.oauthClientAppRepo = repo
}

const SettingKeyOAuthServerRedirectURIs = "oauth_server_redirect_uris"

// OAuthServerSettings never exposes the client secret to the browser.
type OAuthServerSettings struct {
	ClientID         string   `json:"client_id"`
	SecretConfigured bool     `json:"secret_configured"`
	RedirectURIs     []string `json:"redirect_uris"`
	Source           string   `json:"source"`
}

func isOAuthLoopback(u *url.URL) bool {
	return u.Scheme == "http" && u.Port() != "" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}

func ValidateOAuthServerRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "*\\\r\n\t ") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") || (u.Scheme != "https" && !isOAuthLoopback(u)) {
		return fmt.Errorf("回调必须是完整 HTTPS 地址（本地回调可用 HTTP），不能包含通配符、账号或片段")
	}
	return nil
}

func OAuthServerRedirectAllowed(allowed []string, requested string) bool {
	if len(allowed) == 0 || ValidateOAuthServerRedirectURI(requested) != nil {
		return false
	}
	for _, uri := range allowed {
		if uri == requested {
			return true
		}
	}
	u, _ := url.Parse(requested)
	return isOAuthLoopback(u)
}

func (s *SettingService) GetOAuthServerSettings(ctx context.Context) (*OAuthServerSettings, error) {
	result := &OAuthServerSettings{ClientID: s.cfg.OAuthServer.ClientID, SecretConfigured: s.cfg.OAuthServer.ClientSecret != "", RedirectURIs: []string{}, Source: "environment"}
	// The original settings page edits the same default client as the app registry.
	if s.oauthClientAppRepo != nil {
		app, err := s.oauthClientAppRepo.GetByClientID(ctx, result.ClientID)
		if err == nil {
			result.RedirectURIs = append([]string{}, app.RedirectURIs...)
			result.SecretConfigured = app.ClientSecret != ""
			result.Source = "database"
			return result, nil
		}
		if !errors.Is(err, ErrOAuthClientNotFound) {
			return nil, err
		}
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyOAuthServerRedirectURIs)
	if errors.Is(err, ErrSettingNotFound) {
		if uri := strings.TrimSpace(s.cfg.OAuthServer.RedirectURI); uri != "" {
			result.RedirectURIs = []string{uri}
		}
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &result.RedirectURIs); err != nil {
		return nil, fmt.Errorf("invalid OAuth redirect settings: %w", err)
	}
	if result.RedirectURIs == nil {
		result.RedirectURIs = []string{}
	}
	result.Source = "database"
	return result, nil
}

func (s *SettingService) SetOAuthServerRedirectURIs(ctx context.Context, uris []string) (*OAuthServerSettings, error) {
	if len(uris) > 50 {
		return nil, fmt.Errorf("最多配置 50 个回调地址")
	}
	normalized := []string{}
	seen := map[string]bool{}
	for _, value := range uris {
		uri := strings.TrimSpace(value)
		if len(uri) > 2048 {
			return nil, fmt.Errorf("回调地址过长")
		}
		if err := ValidateOAuthServerRedirectURI(uri); err != nil {
			return nil, err
		}
		if !seen[uri] {
			normalized = append(normalized, uri)
			seen[uri] = true
		}
	}
	if s.oauthClientAppRepo != nil {
		app, err := s.oauthClientAppRepo.GetByClientID(ctx, s.cfg.OAuthServer.ClientID)
		if err == nil {
			app.RedirectURIs = normalized
			if err := s.oauthClientAppRepo.Update(ctx, app); err != nil {
				return nil, err
			}
			return s.GetOAuthServerSettings(ctx)
		}
		if !errors.Is(err, ErrOAuthClientNotFound) {
			return nil, err
		}
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	if err := s.settingRepo.Set(ctx, SettingKeyOAuthServerRedirectURIs, string(raw)); err != nil {
		return nil, err
	}
	return s.GetOAuthServerSettings(ctx)
}

func (s *SettingService) IsOAuthServerRedirectAllowed(ctx context.Context, requested string) (bool, error) {
	settings, err := s.GetOAuthServerSettings(ctx)
	if err != nil {
		return false, err
	}
	return OAuthServerRedirectAllowed(settings.RedirectURIs, requested), nil
}
