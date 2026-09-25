package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const claudeSubscriptionProfileURL = "https://api.anthropic.com/api/oauth/profile"
const claudeSubscriptionProfileMaxBytes = 1024 * 1024

func (s *claudeOAuthService) FetchSubscriptionProfile(ctx context.Context, accessToken, proxyURL string) (*service.ClaudeSubscriptionProfile, error) {
	client, err := s.clientFactory(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("create Claude profile client: %w", err)
	}
	// Subscription evidence must come from the fixed official endpoint, never a
	// caller-selected host or a redirect response from another authority.
	client.GetClient().CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := client.R().SetContext(ctx).
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetHeader("Cache-Control", "no-cache").
		SetHeader("User-Agent", defaultUsageUserAgent).
		DisableAutoReadResponse().Get(claudeSubscriptionProfileURL)
	if err != nil {
		return nil, fmt.Errorf("Claude profile request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Claude profile returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, claudeSubscriptionProfileMaxBytes+1))
	if err != nil || len(body) > claudeSubscriptionProfileMaxBytes {
		return nil, fmt.Errorf("invalid Claude profile response size")
	}
	var profile service.ClaudeSubscriptionProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, fmt.Errorf("invalid Claude profile response")
	}
	return &profile, nil
}
