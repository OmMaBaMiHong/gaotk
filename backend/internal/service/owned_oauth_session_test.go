package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestOwnedOAuthExchangeRejectsForeignSessionBeforeProviderCalls(t *testing.T) {
	ctx := WithOwnedAccountScope(context.Background(), 42, nil)
	t.Run("claude", func(t *testing.T) {
		store := oauth.NewSessionStore()
		defer store.Stop()
		store.Set("foreign", &oauth.OAuthSession{OwnerUserID: 43, CreatedAt: time.Now()})
		svc := &OAuthService{sessionStore: store}
		_, err := svc.ExchangeCode(ctx, &ExchangeCodeInput{SessionID: "foreign", Code: "code"})
		require.ErrorContains(t, err, "Authorization session not found")
		_, ok := store.Get("foreign")
		require.True(t, ok, "foreign failure must not consume another owner's session")
	})
	t.Run("openai", func(t *testing.T) {
		store := openai.NewSessionStore()
		defer store.Stop()
		store.Set("foreign", &openai.OAuthSession{OwnerUserID: 43, CreatedAt: time.Now()})
		svc := &OpenAIOAuthService{sessionStore: store}
		_, err := svc.ExchangeCode(ctx, &OpenAIExchangeCodeInput{SessionID: "foreign", Code: "code"})
		require.ErrorContains(t, err, "Authorization session not found")
		_, ok := store.Get("foreign")
		require.True(t, ok, "foreign failure must not consume another owner's session")
	})
	t.Run("gemini", func(t *testing.T) {
		store := geminicli.NewSessionStore()
		defer store.Stop()
		store.Set("foreign", &geminicli.OAuthSession{OwnerUserID: 43, CreatedAt: time.Now()})
		svc := &GeminiOAuthService{sessionStore: store}
		_, err := svc.ExchangeCode(ctx, &GeminiExchangeCodeInput{SessionID: "foreign", Code: "code"})
		require.ErrorContains(t, err, "Authorization session not found")
		_, ok := store.Get("foreign")
		require.True(t, ok, "foreign failure must not consume another owner's session")
	})
	t.Run("antigravity", func(t *testing.T) {
		store := antigravity.NewSessionStore()
		defer store.Stop()
		store.Set("foreign", &antigravity.OAuthSession{OwnerUserID: 43, CreatedAt: time.Now()})
		svc := &AntigravityOAuthService{sessionStore: store}
		_, err := svc.ExchangeCode(ctx, &AntigravityExchangeCodeInput{SessionID: "foreign", Code: "code"})
		require.ErrorContains(t, err, "Authorization session not found")
		_, ok := store.Get("foreign")
		require.True(t, ok, "foreign failure must not consume another owner's session")
	})
	t.Run("grok", func(t *testing.T) {
		store := xai.NewSessionStore()
		defer store.Stop()
		store.Set("foreign", &xai.OAuthSession{OwnerUserID: 43, CreatedAt: time.Now()})
		svc := &GrokOAuthService{sessionStore: store}
		_, err := svc.ExchangeCode(ctx, &GrokExchangeCodeInput{SessionID: "foreign", Code: "code"})
		require.ErrorContains(t, err, "Authorization session not found")
		_, ok := store.Get("foreign")
		require.True(t, ok, "foreign failure must not consume another owner's session")
	})
	t.Run("kimi", func(t *testing.T) {
		store := newKimiDeviceFlowStore()
		store.Set("foreign", &kimiDeviceSession{ownerUserID: 43, expiresAt: time.Now().Add(time.Minute)})
		svc := &KimiOAuthService{deviceStore: store}
		_, err := svc.PollDeviceFlow(ctx, "foreign")
		require.ErrorContains(t, err, "Authorization session not found")
		_, err = svc.ConsumeDeviceFlow(ctx, "foreign")
		require.ErrorContains(t, err, "Authorization session not found")
		_, ok := store.Get("foreign")
		require.True(t, ok)
	})
}
