package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type mergedOAuthClientRepo struct {
	OAuthClientAppRepository
	app *OAuthClientApp
}

func (r *mergedOAuthClientRepo) Count(context.Context) (int, error) {
	if r.app != nil {
		return 1, nil
	}
	return 0, nil
}

func (r *mergedOAuthClientRepo) Create(_ context.Context, app *OAuthClientApp) error {
	r.app = app
	return nil
}

func (r *mergedOAuthClientRepo) GetByClientID(_ context.Context, id string) (*OAuthClientApp, error) {
	if r.app == nil || r.app.ClientID != id {
		return nil, ErrOAuthClientNotFound
	}
	app := *r.app
	return &app, nil
}

func (r *mergedOAuthClientRepo) GetByID(ctx context.Context, _ int64) (*OAuthClientApp, error) {
	return r.GetByClientID(ctx, r.app.ClientID)
}

func (r *mergedOAuthClientRepo) Update(_ context.Context, app *OAuthClientApp) error {
	r.app = app
	return nil
}

func TestOAuthClientMergePreservesSavedCallbacksAndBothEditors(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{OAuthServer: config.OAuthServerConfig{
		ClientID: "skoob", ClientSecret: "existing-private-secret", RedirectURI: "https://old.example/callback",
	}}
	settings := NewSettingService(&panelRateLimitSettingRepo{}, cfg)
	_, err := settings.SetOAuthServerRedirectURIs(ctx, []string{"https://saved.example/callback"})
	require.NoError(t, err)
	repo := &mergedOAuthClientRepo{}
	settings.SetOAuthClientAppRepository(repo)
	clients := NewOAuthClientAppService(repo, cfg, settings)
	require.NotNil(t, repo.app)
	require.Equal(t, cfg.OAuthServer.ClientSecret, repo.app.ClientSecret)
	require.Equal(t, []string{"https://saved.example/callback"}, repo.app.RedirectURIs)
	require.False(t, repo.app.IsAllowedRedirectURI(cfg.OAuthServer.RedirectURI))

	_, err = settings.SetOAuthServerRedirectURIs(ctx, []string{"https://settings.example/callback"})
	require.NoError(t, err)
	app, err := clients.GetEnabledByClientID(ctx, "skoob")
	require.NoError(t, err)
	require.True(t, app.IsAllowedRedirectURI("https://settings.example/callback"))
	require.False(t, app.IsAllowedRedirectURI("https://saved.example/callback"))

	uri := "https://registry.example/callback"
	_, err = clients.Update(ctx, 1, &UpdateOAuthClientAppInput{RedirectURIs: &uri})
	require.NoError(t, err)
	saved, err := settings.GetOAuthServerSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{uri}, saved.RedirectURIs)

	_, err = settings.SetOAuthServerRedirectURIs(ctx, nil)
	require.NoError(t, err)
	require.False(t, repo.app.IsAllowedRedirectURI(uri))
	require.False(t, repo.app.IsAllowedRedirectURI("http://localhost:9002/callback"))
}

func TestOAuthClientMergePreservesStrictRedirectValidation(t *testing.T) {
	repo := &mergedOAuthClientRepo{}
	clients := NewOAuthClientAppService(repo, &config.Config{}, nil)
	app, err := clients.Create(context.Background(), &CreateOAuthClientAppInput{
		Name: "Test", ClientID: "test", RedirectURIs: "https://valid.example/callback", AllowLocalhost: true,
	})
	require.NoError(t, err)
	require.True(t, app.IsAllowedRedirectURI("http://localhost:9002/callback"))
	require.True(t, app.IsAllowedRedirectURI("http://[::1]:9002/callback"))
	for _, uri := range []string{"http://localhost:9002@evil.example/callback", "https://user:pass@valid.example/callback", "https://valid.example/callback#x", "https://*.example/callback"} {
		require.False(t, app.IsAllowedRedirectURI(uri), uri)
		_, err := clients.Update(context.Background(), app.ID, &UpdateOAuthClientAppInput{RedirectURIs: &uri})
		require.Error(t, err, uri)
		require.Equal(t, []string{"https://valid.example/callback"}, repo.app.RedirectURIs)
	}
}
