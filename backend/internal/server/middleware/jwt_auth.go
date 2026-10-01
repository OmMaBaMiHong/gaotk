package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// NewJWTAuthMiddleware 创建 JWT 认证中间件
func NewJWTAuthMiddleware(
	authService *service.AuthService,
	userService *service.UserService,
	settingService *service.SettingService,
	auditService *service.AuditLogService,
) JWTAuthMiddleware {
	return JWTAuthMiddleware(jwtAuth(authService, userService, userService, settingService, auditService))
}

type jwtUserReader interface {
	GetByID(ctx context.Context, id int64) (*service.User, error)
}

type userActivityToucher interface {
	TouchLastActiveForUser(ctx context.Context, user *service.User)
}

// jwtAuth JWT认证中间件实现
func jwtAuth(
	authService *service.AuthService,
	userService jwtUserReader,
	activityToucher userActivityToucher,
	settingService *service.SettingService,
	auditService *service.AuditLogService,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从Authorization header中提取token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			AbortWithError(c, 401, "UNAUTHORIZED", "Authorization header is required")
			return
		}

		// 验证Bearer scheme
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			AbortWithError(c, 401, "INVALID_AUTH_HEADER", "Authorization header format must be 'Bearer {token}'")
			return
		}

		tokenString := strings.TrimSpace(parts[1])
		if tokenString == "" {
			AbortWithError(c, 401, "EMPTY_TOKEN", "Token cannot be empty")
			return
		}

		// 验证token
		claims, err := authService.ValidateToken(tokenString)
		if err != nil {
			if errors.Is(err, service.ErrTokenExpired) {
				AbortWithError(c, 401, "TOKEN_EXPIRED", "Token has expired")
				return
			}
			AbortWithError(c, 401, "INVALID_TOKEN", "Invalid token")
			return
		}

		// 从数据库获取最新的用户信息
		user, err := userService.GetByID(c.Request.Context(), claims.UserID)
		if err != nil {
			if errors.Is(err, service.ErrUserNotFound) {
				AbortWithError(c, 401, "USER_NOT_FOUND", "User not found")
			} else {
				AbortWithError(c, 500, "INTERNAL_ERROR", "Failed to load user")
			}
			return
		}

		// 检查用户状态
		if !user.IsActive() {
			AbortWithError(c, 401, "USER_INACTIVE", "User account is not active")
			return
		}

		// Security: Validate TokenVersion to ensure token hasn't been invalidated
		// This check ensures tokens issued before a password change are rejected
		if claims.TokenVersion != user.TokenVersion {
			AbortWithError(c, 401, "TOKEN_REVOKED", "Token has been revoked (password changed)")
			return
		}
		if claims.Scope != "" {
			revoked, err := authService.IsAccessSessionRevoked(c.Request.Context(), claims.SessionID)
			if err != nil {
				AbortWithError(c, 503, "SESSION_CHECK_UNAVAILABLE", "Session status unavailable")
				return
			}
			if revoked {
				AbortWithError(c, 401, "TOKEN_REVOKED", "Token has been revoked")
				return
			}
			epoch, err := authService.GetUserTokenEpoch(c.Request.Context(), claims.UserID)
			if err != nil {
				AbortWithError(c, 503, "SESSION_CHECK_UNAVAILABLE", "Session status unavailable")
				return
			}
			if claims.RevocationEpoch != epoch {
				AbortWithError(c, 401, "TOKEN_REVOKED", "Token has been revoked")
				return
			}
		}

		// OAuth 受限令牌：只放行 scope 白名单内的端点，面板管理 API 一律 403。
		// 这是"第三方应用拿到的不是全量钥匙"的执行点。
		if claims.Scope != "" && !oauthScopeAllowsEndpoint(claims.Scope, c.Request.Method, c.FullPath()) {
			AbortWithError(c, 403, "OAUTH_SCOPE_FORBIDDEN", "Token scope does not allow this endpoint")
			return
		}

		// 会话绑定校验：IP/UA 任一变化即撤销会话（功能可在系统设置中关闭）
		if !enforceSessionBinding(c, authService, settingService, auditService, claims) {
			return
		}

		c.Set(string(ContextKeyUser), AuthSubject{
			UserID:      user.ID,
			Concurrency: user.Concurrency,
		})
		c.Set(string(ContextKeyUserRole), user.Role)
		c.Set(ContextKeyAuthEmail, user.Email)
		c.Set(ContextKeySessionID, claims.SessionID)
		c.Set(string(ContextKeyOAuthScope), claims.Scope)
		if activityToucher != nil {
			activityToucher.TouchLastActiveForUser(c.Request.Context(), user)
		}

		c.Next()
	}
}

// oauthScopeEndpoints 每个 scope 允许访问的端点白名单：scope → 路由 FullPath → 允许的 HTTP 方法。
// profile = 身份信息；membership = 会员态与在售套餐只读；keys = 用户自己的 API Key 管理
// （skoob 等 C 端"领 key/登录对账"的最小集，仍远小于全量面板令牌）。
// 端点用 gin 注册路由的 FullPath 精确匹配，新增授权端点必须显式登记到这里。
var oauthScopeEndpoints = map[string]map[string]map[string]struct{}{ //nolint:gochecknoglobals // 静态白名单，编译期可知
	"profile": {
		"/api/v1/auth/me": {http.MethodGet: {}},
	},
	"membership": {
		"/api/v1/auth/me":                {http.MethodGet: {}},
		"/api/v1/subscriptions":          {http.MethodGet: {}},
		"/api/v1/subscriptions/active":   {http.MethodGet: {}},
		"/api/v1/subscriptions/progress": {http.MethodGet: {}},
		"/api/v1/subscriptions/summary":  {http.MethodGet: {}},
		"/api/v1/payment/plans":          {http.MethodGet: {}},
	},
	"keys": {
		"/api/v1/keys":     {http.MethodGet: {}, http.MethodPost: {}},
		"/api/v1/keys/:id": {http.MethodGet: {}, http.MethodDelete: {}},
	},
}

// oauthScopeAllowsEndpoint 判断携带 scopes 的受限令牌能否访问 method+fullPath。
// 未登记的方法/端点/未知 scope 一律拒绝（fail closed）。
func oauthScopeAllowsEndpoint(scopes string, method string, fullPath string) bool {
	if fullPath == "" {
		fullPath = "/api/v1/auth/me"
	}
	for _, scope := range strings.Split(scopes, ",") {
		paths, ok := oauthScopeEndpoints[strings.TrimSpace(scope)]
		if !ok {
			continue
		}
		methods, hit := paths[fullPath]
		if !hit {
			continue
		}
		if _, allowed := methods[method]; allowed {
			return true
		}
	}
	return false
}

// Deprecated: prefer GetAuthSubjectFromContext in auth_subject.go.
