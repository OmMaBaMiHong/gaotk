package domain

import (
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

// OAuthClientApp 第三方应用接入中转站 OAuth 的客户端注册信息。
type OAuthClientApp struct {
	ID             int64
	Name           string
	ClientID       string
	ClientSecret   string
	RedirectURIs   []string
	AllowLocalhost bool
	Enabled        bool
	Remark         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
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
	for _, uri := range app.RedirectURIs {
		if uri == requested {
			return true
		}
	}
	// 本地开发场景：应用显式开启后，放行 localhost/127.0.0.1/[::1] 任意端口。
	if app.AllowLocalhost {
		if strings.HasPrefix(requested, "http://127.0.0.1:") ||
			strings.HasPrefix(requested, "http://localhost:") ||
			strings.HasPrefix(requested, "http://[::1]:") {
			return true
		}
	}
	return false
}
