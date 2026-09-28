package domain

import (
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// OAuth 授权应用（第三方应用接入中转站 OAuth 的客户端注册）相关领域错误。
var (
	ErrOAuthClientNotFound      = infraerrors.NotFound("OAUTH_CLIENT_NOT_FOUND", "oauth client not found")
	ErrOAuthClientDisabled      = infraerrors.Forbidden("OAUTH_CLIENT_DISABLED", "oauth client is disabled")
	ErrOAuthClientIDDuplicated  = infraerrors.Conflict("OAUTH_CLIENT_ID_DUPLICATED", "client_id already exists")
	ErrOAuthClientNameRequired  = infraerrors.BadRequest("OAUTH_CLIENT_NAME_REQUIRED", "应用名称不能为空")
	ErrOAuthClientIDInvalid     = infraerrors.BadRequest("OAUTH_CLIENT_ID_INVALID", "client_id 仅允许字母、数字、下划线和连字符（1-64 位）")
	ErrOAuthClientURIsRequired  = infraerrors.BadRequest("OAUTH_CLIENT_URI_REQUIRED", "至少登记一个回调地址")
	ErrOAuthClientSecretInvalid = infraerrors.BadRequest("OAUTH_CLIENT_SECRET_INVALID", "client_secret 长度需在 16-128 位之间")
)

// OAuth 授权 scope 约定。
//
// 受限模式：客户端登记了 allowed_scopes 后，authorize 校验请求的 scope，
// token 端点签发只带这些 scope 的短时令牌（不能访问面板管理 API）。
// 空 allowed_scopes = 传统模式，code 换全量面板令牌（存量客户端零影响）。
const (
	OAuthScopeProfile    = "profile"    // 身份信息（/auth/me 裁剪视图）
	OAuthScopeMembership = "membership" // 会员态只读（/subscriptions、/payment/plans 只读端点）
	OAuthScopeKeys       = "keys"       // 自己的 API Key 管理（GET/POST/DELETE /keys）——C 端"领 key/对账"能力
)

// KnownOAuthScopes 全部合法 scope。登记与请求时都按这个集合校验，
// 防止拼写错误的 scope 静默变成"什么都查不到"。
var KnownOAuthScopes = map[string]struct{}{
	OAuthScopeProfile:    {},
	OAuthScopeMembership: {},
	OAuthScopeKeys:       {},
}

// ErrOAuthClientScopeInvalid scope 不合法（不在 KnownOAuthScopes 或超出客户端白名单）。
var ErrOAuthClientScopeInvalid = infraerrors.BadRequest("OAUTH_CLIENT_SCOPE_INVALID", "scope 不合法或超出该应用登记的 scope 白名单")

// OAuthClientApp 第三方应用接入中转站 OAuth 的客户端注册信息。
type OAuthClientApp struct {
	ID             int64
	Name           string
	ClientID       string
	ClientSecret   string
	RedirectURIs   []string
	AllowedScopes  []string
	AllowLocalhost bool
	Enabled        bool
	Remark         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// IsScoped 是否为受限模式客户端（登记过 scope 白名单）。
func (app *OAuthClientApp) IsScoped() bool {
	return len(app.AllowedScopes) > 0
}

// NormalizeOAuthScopes 拆分并清洗 scope 原文（逗号/空格分隔），小写、去空、去重、保序。
func NormalizeOAuthScopes(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == '\t' })
	seen := make(map[string]struct{}, len(fields))
	scopes := make([]string, 0, len(fields))
	for _, f := range fields {
		s := strings.ToLower(strings.TrimSpace(f))
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		scopes = append(scopes, s)
	}
	return scopes
}

// ResolveRequestedScopes 把 authorize 请求里的 scope 参数解析成本次授权的 scope 集合。
// 传统模式客户端：忽略请求参数，返回 nil（签发全量令牌）。
// 受限模式客户端：请求为空 → 默认授予全部登记 scope；非空 → 必须是登记集合的子集。
func (app *OAuthClientApp) ResolveRequestedScopes(requested string) ([]string, error) {
	if !app.IsScoped() {
		return nil, nil
	}
	requestedScopes := NormalizeOAuthScopes(requested)
	for _, s := range requestedScopes {
		if _, known := KnownOAuthScopes[s]; !known {
			return nil, ErrOAuthClientScopeInvalid
		}
		allowed := false
		for _, a := range app.AllowedScopes {
			if a == s {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, ErrOAuthClientScopeInvalid
		}
	}
	if len(requestedScopes) == 0 {
		return append([]string(nil), app.AllowedScopes...), nil
	}
	return requestedScopes, nil
}

// ValidateAllowedScopes 登记侧校验：scope 白名单里的每一项必须是已知 scope。
func ValidateAllowedScopes(scopes []string) error {
	for _, s := range scopes {
		if _, known := KnownOAuthScopes[s]; !known {
			return ErrOAuthClientScopeInvalid
		}
	}
	return nil
}

// NormalizeRedirectURIs 拆分并清洗回调白名单（支持换行/逗号分隔），去空去重、保序。
func NormalizeRedirectURIs(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' })
	seen := make(map[string]struct{}, len(fields))
	uris := make([]string, 0, len(fields))
	for _, f := range fields {
		uri := strings.TrimSpace(f)
		if uri == "" {
			continue
		}
		if _, dup := seen[uri]; dup {
			continue
		}
		seen[uri] = struct{}{}
		uris = append(uris, uri)
	}
	return uris
}

// IsAllowedRedirectURI 判断 requested 是否命中该应用的回调白名单。
func (app *OAuthClientApp) IsAllowedRedirectURI(requested string) bool {
	u, err := url.Parse(requested)
	if err != nil || len(app.RedirectURIs) == 0 || strings.TrimSpace(requested) != requested || strings.ContainsAny(requested, "*\\\r\n\t ") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(requested, "#") {
		return false
	}
	loopback := u.Scheme == "http" && u.Port() != "" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
	if u.Scheme != "https" && !loopback {
		return false
	}
	for _, uri := range app.RedirectURIs {
		if uri == requested {
			return true
		}
	}
	// 本地开发场景：应用显式开启后，放行 localhost/127.0.0.1/[::1] 任意端口。
	if app.AllowLocalhost && loopback {
		return true
	}
	return false
}
