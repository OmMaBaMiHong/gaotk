package service

import (
	"context"
	"strings"
	"time"
)

// KimiTokenRefresher 处理 Kimi OAuth token 刷新。
type KimiTokenRefresher struct {
	kimiOAuthService *KimiOAuthService
}

// NewKimiTokenRefresher 创建 Kimi token 刷新器。
func NewKimiTokenRefresher(kimiOAuthService *KimiOAuthService) *KimiTokenRefresher {
	return &KimiTokenRefresher{kimiOAuthService: kimiOAuthService}
}

// CacheKey 返回用于分布式锁的缓存键。
func (r *KimiTokenRefresher) CacheKey(account *Account) string {
	return KimiTokenCacheKey(account)
}

// CanRefresh 检查是否能处理此账号：kimi 平台的 OAuth 账号。
func (r *KimiTokenRefresher) CanRefresh(account *Account) bool {
	return account.Platform == PlatformKimi && account.Type == AccountTypeOAuth
}

// NeedsRefresh 检查 token 是否需要刷新。
// refresh_token 缺失时视为不可刷新；基于 expires_at 判断是否进入刷新窗口。
func (r *KimiTokenRefresher) NeedsRefresh(account *Account, refreshWindow time.Duration) bool {
	if !r.CanRefresh(account) {
		return false
	}
	if strings.TrimSpace(account.GetCredential("refresh_token")) == "" {
		return false
	}
	expiresAt := account.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		return false
	}
	return time.Until(*expiresAt) < refreshWindow
}

// Refresh 执行 token 刷新并保留原 credentials 中的非 token 字段。
func (r *KimiTokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	tokenInfo, err := r.kimiOAuthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return nil, err
	}
	newCredentials := r.kimiOAuthService.BuildAccountCredentials(tokenInfo)
	return MergeCredentials(account.Credentials, newCredentials), nil
}