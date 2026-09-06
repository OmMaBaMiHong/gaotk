package kimi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
)

// Kimi Code 设备授权流常量。
// client_id 为 Kimi Code CLI 使用的公开客户端，scope 固定为 kimi-code。
const (
	ClientID = "17e5f671-d194-4dfb-9706-5516cb48c098"
	Scope    = "kimi-code"

	DeviceAuthorizationURL = "https://auth.kimi.com/api/oauth/device_authorization"
	TokenURL               = "https://auth.kimi.com/api/oauth/token"

	// DeviceCodeGrantType 为 RFC 8628 定义的设备授权 grant type。
	DeviceCodeGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	UserAgent = "kimi-code/1.0"
)

// OAuth 设备授权轮询的错误状态码。
var (
	ErrAuthorizationPending = errors.New("authorization_pending")
	ErrSlowDown             = errors.New("slow_down")
	ErrAccessDenied         = errors.New("access_denied")
	ErrExpiredToken         = errors.New("expired_token")
)

// DeviceAuthorizationData 是 device_authorization 端点的响应。
type DeviceAuthorizationData struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

// TokenResponse 是 token 端点的成功响应。
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

// tokenErrorResponse 是 token 端点的错误响应（通常仍为 HTTP 200）。
type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func postForm(ctx context.Context, endpoint string, form url.Values, out any) ([]byte, int, error) {
	client, err := httpclient.GetClient(httpclient.Options{Timeout: 30 * time.Second})
	if err != nil {
		return nil, 0, fmt.Errorf("create kimi oauth http client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, fmt.Errorf("build kimi oauth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("kimi oauth request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read kimi oauth response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// 设备授权等端点可能在非 2xx 下返回 RFC 8628 错误码（如 authorization_pending，
		// 常以 HTTP 400 返回）。这里仍尽量把 body 解析进 out，便于调用方按 error 字段分类。
		if out != nil && len(body) > 0 {
			_ = json.Unmarshal(body, out)
		}
		return body, resp.StatusCode, fmt.Errorf("kimi oauth HTTP %d: %s", resp.StatusCode, string(body))
	}

	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return nil, resp.StatusCode, fmt.Errorf("parse kimi oauth response: %w", err)
		}
	}
	return body, resp.StatusCode, nil
}

// StartDeviceAuthorization 发起设备授权，返回需展示给用户的 device/user code 与验证地址。
func StartDeviceAuthorization(ctx context.Context) (*DeviceAuthorizationData, error) {
	form := url.Values{}
	form.Set("client_id", ClientID)
	form.Set("scope", Scope)

	var data DeviceAuthorizationData
	if _, _, err := postForm(ctx, DeviceAuthorizationURL, form, &data); err != nil {
		return nil, err
	}
	if data.DeviceCode == "" {
		return nil, errors.New("kimi device_authorization response missing device_code")
	}
	return &data, nil
}

// PollToken 轮询 token 端点直到用户完成授权（或失败/过期）。
// 返回 pendingSlowed 表示仍在等待用户授权，需要按 interval 稍后重试。
func PollToken(ctx context.Context, deviceCode string) (resp *TokenResponse, pendingSlowed bool, err error) {
	form := url.Values{}
	form.Set("grant_type", DeviceCodeGrantType)
	form.Set("device_code", deviceCode)
	form.Set("client_id", ClientID)

	var out struct {
		TokenResponse
		tokenErrorResponse
	}
	if _, _, err := postForm(ctx, TokenURL, form, &out); err != nil {
		// 设备授权轮询：授权服务器可能以 HTTP 400 + JSON 错误体返回 pending 等设备流错误码，
		// postForm 已尽力把 body 解析进 out，这里优先按 out.Error 分类，避免把 pending 当硬错误。
		if out.Error != "" {
			switch out.Error {
			case "authorization_pending":
				return nil, true, ErrAuthorizationPending
			case "slow_down":
				return nil, true, ErrSlowDown
			case "access_denied":
				return nil, false, ErrAccessDenied
			case "expired_token":
				return nil, false, ErrExpiredToken
			default:
				return nil, false, fmt.Errorf("kimi token error: %s: %s", out.Error, out.ErrorDescription)
			}
		}
		return nil, false, err
	}

	switch out.Error {
	case "":
		if out.AccessToken == "" {
			return nil, false, errors.New("kimi token response missing access_token")
		}
		return &out.TokenResponse, false, nil
	case "authorization_pending":
		return nil, true, ErrAuthorizationPending
	case "slow_down":
		return nil, true, ErrSlowDown
	case "access_denied":
		return nil, false, ErrAccessDenied
	case "expired_token":
		return nil, false, ErrExpiredToken
	default:
		return nil, false, fmt.Errorf("kimi token error: %s: %s", out.Error, out.ErrorDescription)
	}
}

// RefreshToken 使用 refresh_token 换取新的 access_token。
func RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, errors.New("kimi refresh_token is required")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", ClientID)

	var out struct {
		TokenResponse
		tokenErrorResponse
	}
	if _, _, err := postForm(ctx, TokenURL, form, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("kimi token refresh error: %s: %s", out.Error, out.ErrorDescription)
	}
	if out.AccessToken == "" {
		return nil, errors.New("kimi token refresh response missing access_token")
	}
	return &out.TokenResponse, nil
}