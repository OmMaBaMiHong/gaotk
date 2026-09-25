package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOAuthServerSettingsRuntimeUpdate(t *testing.T) {
	ctx := context.Background()
	repo := &panelRateLimitSettingRepo{}
	svc := NewSettingService(repo, &config.Config{OAuthServer: config.OAuthServerConfig{ClientID: "skoob", ClientSecret: "private", RedirectURI: "https://old.example/callback"}})
	initial, err := svc.GetOAuthServerSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"https://old.example/callback"}, initial.RedirectURIs)
	require.True(t, initial.SecretConfigured)
	require.Equal(t, "environment", initial.Source)
	saved, err := svc.SetOAuthServerRedirectURIs(ctx, []string{"https://old.example/callback", " https://skoob.cc/api/v1/account/oauth/callback ", "https://old.example/callback"})
	require.NoError(t, err)
	require.Len(t, saved.RedirectURIs, 2)
	require.Equal(t, "database", saved.Source)
	for _, uri := range saved.RedirectURIs {
		allowed, err := svc.IsOAuthServerRedirectAllowed(ctx, uri)
		require.NoError(t, err)
		require.True(t, allowed)
	}
	_, err = svc.SetOAuthServerRedirectURIs(ctx, []string{})
	require.NoError(t, err)
	allowed, err := svc.IsOAuthServerRedirectAllowed(ctx, "https://old.example/callback")
	require.NoError(t, err)
	require.False(t, allowed, "empty stored list must not restore environment callback")
	repo.getValueErr = errors.New("database unavailable")
	_, err = svc.IsOAuthServerRedirectAllowed(ctx, "https://old.example/callback")
	require.Error(t, err)
}

func TestOAuthServerRedirectValidation(t *testing.T) {
	for _, uri := range []string{"https://skoob.cc/callback", "http://127.0.0.1:4579/callback", "http://localhost:9002/callback", "http://[::1]:9002/callback"} {
		require.NoError(t, ValidateOAuthServerRedirectURI(uri), uri)
	}
	for _, uri := range []string{"http://skoob.cc/callback", "https://*.skoob.cc/callback", "https://skoob.cc/callback#x", "https://user:pass@skoob.cc/callback", "http://localhost:123@evil.example/callback", "//skoob.cc/callback", "javascript:alert(1)", "https://skoob.cc/callback\n"} {
		require.Error(t, ValidateOAuthServerRedirectURI(uri), uri)
	}
	repo := &panelRateLimitSettingRepo{}
	svc := NewSettingService(repo, &config.Config{OAuthServer: config.OAuthServerConfig{RedirectURI: "https://skoob.cc/callback"}})
	for _, uri := range []string{"http://localhost:9876/callback", "http://127.0.0.1:4321/callback"} {
		allowed, err := svc.IsOAuthServerRedirectAllowed(context.Background(), uri)
		require.NoError(t, err)
		require.True(t, allowed)
	}
	allowed, err := svc.IsOAuthServerRedirectAllowed(context.Background(), "https://skoob.cc.evil.example/callback")
	require.NoError(t, err)
	require.False(t, allowed)
	_, err = svc.SetOAuthServerRedirectURIs(context.Background(), []string{"https://*.skoob.cc/callback"})
	require.Error(t, err)
	require.Empty(t, repo.values)
}
