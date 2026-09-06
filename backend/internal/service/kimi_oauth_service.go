package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/kimi"
)

// kimiDeviceFlowStore 内存存储设备授权轮询会话（device_code 等），单实例运行即可。
type kimiDeviceFlowStore struct {
	mu       sync.RWMutex
	sessions map[string]*kimiDeviceSession
}

type kimiDeviceSession struct {
	deviceCode string
	expiresAt  time.Time
	// tokenInfo 在轮询成功后缓存，供 create-from-oauth 消费；避免轮询与创建两个步骤争抢同一个会话。
	tokenInfo *KimiTokenInfo
}

func newKimiDeviceFlowStore() *kimiDeviceFlowStore {
	return &kimiDeviceFlowStore{sessions: make(map[string]*kimiDeviceSession)}
}

func (s *kimiDeviceFlowStore) Set(id string, session *kimiDeviceSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = session
}

func (s *kimiDeviceFlowStore) Get(id string) (*kimiDeviceSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	if time.Now().After(v.expiresAt) {
		return nil, false
	}
	return v, true
}

func (s *kimiDeviceFlowStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// kimiOAuthTokenSafetyWindow 是 access_token 过期时间的安全余量，避免网络延迟/时钟偏差导致过期后才刷新。
const kimiOAuthTokenSafetyWindow = 300

// KimiOAuthService Kimi Code 设备授权流 OAuth 服务。
type KimiOAuthService struct {
	deviceStore *kimiDeviceFlowStore
}

// NewKimiOAuthService 创建 Kimi OAuth 服务。
func NewKimiOAuthService() *KimiOAuthService {
	return &KimiOAuthService{
		deviceStore: newKimiDeviceFlowStore(),
	}
}

// KimiOAuthCapabilities 暴露给管理端的 Kimi OAuth 支持信息。
type KimiOAuthCapabilities struct {
	Service          string `json:"service"`
	ClientID         string `json:"client_id"`
	Scope            string `json:"scope"`
	VerificationBase string `json:"verification_base"`
}

// GetCapabilities 返回 Kimi OAuth 能力信息。
func (s *KimiOAuthService) GetCapabilities() KimiOAuthCapabilities {
	return KimiOAuthCapabilities{
		Service:          "kimi-code",
		ClientID:         kimi.ClientID,
		Scope:            kimi.Scope,
		VerificationBase: "https://www.kimi.com/code/authorize_device",
	}
}

// KimiStartDeviceFlowResult 发起设备授权后的返回结果。
type KimiStartDeviceFlowResult struct {
	SessionID           string `json:"session_id"`
	UserCode            string `json:"user_code"`
	VerificationURI     string `json:"verification_uri"`
	VerificationURIList string `json:"verification_uri_complete"`
	ExpiresIn           int64  `json:"expires_in"`
	Interval            int64  `json:"interval"`
}

// StartDeviceFlow 请求 Kimi 设备授权码并保存轮询会话。
func (s *KimiOAuthService) StartDeviceFlow(ctx context.Context) (*KimiStartDeviceFlowResult, error) {
	data, err := kimi.StartDeviceAuthorization(ctx)
	if err != nil {
		return nil, err
	}

	sessionID, err := generateKimiSessionID()
	if err != nil {
		return nil, err
	}
	s.deviceStore.Set(sessionID, &kimiDeviceSession{
		deviceCode: data.DeviceCode,
		expiresAt:  time.Now().Add(time.Duration(data.ExpiresIn) * time.Second),
	})

	return &KimiStartDeviceFlowResult{
		SessionID:           sessionID,
		UserCode:            data.UserCode,
		VerificationURI:     data.VerificationURI,
		VerificationURIList: data.VerificationURIComplete,
		ExpiresIn:           data.ExpiresIn,
		Interval:            data.Interval,
	}, nil
}

// KimiPollDeviceFlowResult 轮询结果。
type KimiPollDeviceFlowResult struct {
	Pending   bool            `json:"pending"`
	TokenInfo *KimiTokenInfo `json:"token_info,omitempty"`
}

// PollDeviceFlow 轮询设备授权结果。用户未授权时返回 Pending=true；授权成功后返回 token。
// 轮询成功后只在会话内缓存 tokenInfo，不删除会话——同一会话可供 create-from-oauth 消费，
// 也避免前端的"轮询到成功后再创建"两步之间丢失设备授权结果。
func (s *KimiOAuthService) PollDeviceFlow(ctx context.Context, sessionID string) (*KimiPollDeviceFlowResult, error) {
	session, ok := s.deviceStore.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("kimi device flow session not found or expired")
	}
	if session.tokenInfo != nil {
		return &KimiPollDeviceFlowResult{TokenInfo: session.tokenInfo}, nil
	}

	resp, pending, err := kimi.PollToken(ctx, session.deviceCode)
	if err != nil {
		// 用户拒绝或会话过期：清理会话并返回明确错误。
		if err == kimi.ErrAccessDenied || err == kimi.ErrExpiredToken {
			s.deviceStore.Delete(sessionID)
		}
		return nil, err
	}
	if pending {
		return &KimiPollDeviceFlowResult{Pending: true}, nil
	}

	session.tokenInfo = s.buildTokenInfo(resp)
	return &KimiPollDeviceFlowResult{TokenInfo: session.tokenInfo}, nil
}

// ConsumeDeviceFlow 消费设备授权结果：返回缓存的 token（或轮询一次取回），并删除会话。
// 唯一的消费方，通常由 create-from-oauth 调用。
func (s *KimiOAuthService) ConsumeDeviceFlow(ctx context.Context, sessionID string) (*KimiTokenInfo, error) {
	session, ok := s.deviceStore.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("kimi device flow session not found or expired")
	}
	defer s.deviceStore.Delete(sessionID)

	if session.tokenInfo != nil {
		return session.tokenInfo, nil
	}

	resp, pending, err := kimi.PollToken(ctx, session.deviceCode)
	if err != nil {
		if err == kimi.ErrAccessDenied || err == kimi.ErrExpiredToken {
			return nil, err
		}
		return nil, err
	}
	if pending {
		return nil, kimi.ErrAuthorizationPending
	}
	return s.buildTokenInfo(resp), nil
}

// KimiTokenInfo Kimi OAuth token 信息。
type KimiTokenInfo struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

func (s *KimiOAuthService) buildTokenInfo(resp *kimi.TokenResponse) *KimiTokenInfo {
	now := time.Now().Unix()
	expiresAt := now + resp.ExpiresIn - kimiOAuthTokenSafetyWindow
	if minExpiresAt := now + 30; expiresAt < minExpiresAt {
		expiresAt = minExpiresAt
	}
	return &KimiTokenInfo{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresIn:    resp.ExpiresIn,
		ExpiresAt:    expiresAt,
		TokenType:    resp.TokenType,
		Scope:        resp.Scope,
	}
}

// RefreshToken 使用 refresh_token 刷新，返回带 expires_at 的 token 信息。
func (s *KimiOAuthService) RefreshToken(ctx context.Context, refreshToken string) (*KimiTokenInfo, error) {
	resp, err := kimi.RefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	return s.buildTokenInfo(resp), nil
}

// RefreshAccountToken 刷新指定 Kimi OAuth 账号的 token。
func (s *KimiOAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*KimiTokenInfo, error) {
	if account.Platform != PlatformKimi || account.Type != AccountTypeOAuth {
		return nil, fmt.Errorf("account is not a Kimi OAuth account")
	}
	refreshToken := strings.TrimSpace(account.GetCredential("refresh_token"))
	if refreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}
	return s.RefreshToken(ctx, refreshToken)
}

// BuildAccountCredentials 将 token 信息转换为账号 credentials。
// Kimi 企业坐席为 Coding Plan（subscription），固定 account_mode=coding、api_protocol=anthropic，
// 与 api.kimi.com/coding/v1 上游一致。
func (s *KimiOAuthService) BuildAccountCredentials(tokenInfo *KimiTokenInfo) map[string]any {
	creds := map[string]any{
		"access_token": tokenInfo.AccessToken,
		"expires_at":   fmt.Sprintf("%d", tokenInfo.ExpiresAt),
		"account_mode": AccountModeCoding,
		"api_protocol": APIProtocolAnthropic,
	}
	if tokenInfo.RefreshToken != "" {
		creds["refresh_token"] = tokenInfo.RefreshToken
	}
	if tokenInfo.TokenType != "" {
		creds["token_type"] = tokenInfo.TokenType
	}
	if tokenInfo.Scope != "" {
		creds["scope"] = tokenInfo.Scope
	}
	return creds
}

func generateKimiSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate kimi session id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}