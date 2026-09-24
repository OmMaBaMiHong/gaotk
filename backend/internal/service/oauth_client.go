package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// 领域对象与错误别名（与 announcement 模块同风格）。
type OAuthClientApp = domain.OAuthClientApp

// NormalizeRedirectURIs 供仓储层复用（domain 层实现的别名）。
var NormalizeRedirectURIs = domain.NormalizeRedirectURIs

var (
	ErrOAuthClientNotFound      = domain.ErrOAuthClientNotFound
	ErrOAuthClientDisabled      = domain.ErrOAuthClientDisabled
	ErrOAuthClientIDDuplicated  = domain.ErrOAuthClientIDDuplicated
	ErrOAuthClientNameRequired  = domain.ErrOAuthClientNameRequired
	ErrOAuthClientIDInvalid     = domain.ErrOAuthClientIDInvalid
	ErrOAuthClientURIsRequired  = domain.ErrOAuthClientURIsRequired
	ErrOAuthClientSecretInvalid = domain.ErrOAuthClientSecretInvalid
)

var oauthClientIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// OAuthClientAppRepository 后台客户端注册的持久化接口。
type OAuthClientAppRepository interface {
	Create(ctx context.Context, app *OAuthClientApp) error
	GetByID(ctx context.Context, id int64) (*OAuthClientApp, error)
	GetByClientID(ctx context.Context, clientID string) (*OAuthClientApp, error)
	Update(ctx context.Context, app *OAuthClientApp) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context) ([]*OAuthClientApp, error)
	Count(ctx context.Context) (int, error)
}

// OAuthClientAppService OAuth 授权应用管理（超管 CRUD + 授权/令牌端点的客户端校验）。
type OAuthClientAppService struct {
	repo OAuthClientAppRepository
	// legacy 旧 OAUTH_SERVER_* 环境变量单客户端配置：表为空时引导迁移，保证升级不断登录。
	legacy config.OAuthServerConfig
}

func NewOAuthClientAppService(repo OAuthClientAppRepository, cfg *config.Config) *OAuthClientAppService {
	s := &OAuthClientAppService{repo: repo, legacy: cfg.OAuthServer}
	s.ensureLegacySeeded()
	return s
}

// ensureLegacySeeded 表为空且配置了旧环境变量时，把旧单客户端迁进表里（幂等）。
func (s *OAuthClientAppService) ensureLegacySeeded() {
	if s.legacy.ClientID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	count, err := s.repo.Count(ctx)
	if err != nil {
		slog.Warn("oauth client legacy seed: count failed", "error", err)
		return
	}
	if count > 0 {
		return
	}
	_, err = s.Create(ctx, &CreateOAuthClientAppInput{
		Name:           "Skoob",
		ClientID:       s.legacy.ClientID,
		ClientSecret:   s.legacy.ClientSecret,
		RedirectURIs:   s.legacy.RedirectURI,
		AllowLocalhost: true,
		Remark:         "由 OAUTH_SERVER_* 环境变量自动迁移",
	})
	if err != nil {
		slog.Warn("oauth client legacy seed failed", "error", err)
		return
	}
	slog.Info("oauth client legacy seed done", "client_id", s.legacy.ClientID)
}

type CreateOAuthClientAppInput struct {
	Name           string
	ClientID       string
	ClientSecret   string // 留空则自动生成
	RedirectURIs   string // 换行/逗号分隔的白名单原文
	AllowLocalhost bool
	Remark         string
}

func (s *OAuthClientAppService) Create(ctx context.Context, input *CreateOAuthClientAppInput) (*OAuthClientApp, error) {
	if input == nil {
		return nil, ErrOAuthClientNameRequired
	}
	app, err := s.validateAndFill(input.Name, input.ClientID, input.ClientSecret, input.RedirectURIs, input.AllowLocalhost, input.Remark)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, app); err != nil {
		return nil, err
	}
	return app, nil
}

type UpdateOAuthClientAppInput struct {
	Name           *string
	ClientSecret   *string
	RedirectURIs   *string
	AllowLocalhost *bool
	Enabled        *bool
	Remark         *string
	// RegenerateSecret 置 true 时忽略 ClientSecret，重新生成新密钥。
	RegenerateSecret bool
}

func (s *OAuthClientAppService) Update(ctx context.Context, id int64, input *UpdateOAuthClientAppInput) (*OAuthClientApp, error) {
	if input == nil {
		return nil, ErrOAuthClientNameRequired
	}
	app, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		app.Name = strings.TrimSpace(*input.Name)
	}
	if input.RedirectURIs != nil {
		app.RedirectURIs = domain.NormalizeRedirectURIs(*input.RedirectURIs)
		if len(app.RedirectURIs) == 0 {
			return nil, ErrOAuthClientURIsRequired
		}
	}
	if input.AllowLocalhost != nil {
		app.AllowLocalhost = *input.AllowLocalhost
	}
	if input.Enabled != nil {
		app.Enabled = *input.Enabled
	}
	if input.Remark != nil {
		app.Remark = strings.TrimSpace(*input.Remark)
	}
	if input.RegenerateSecret {
		secret, genErr := generateOAuthClientSecret()
		if genErr != nil {
			return nil, genErr
		}
		app.ClientSecret = secret
	} else if input.ClientSecret != nil {
		secret := strings.TrimSpace(*input.ClientSecret)
		if secret != "" {
			if len(secret) < 16 || len(secret) > 128 {
				return nil, ErrOAuthClientSecretInvalid
			}
			app.ClientSecret = secret
		}
	}
	if strings.TrimSpace(app.Name) == "" {
		return nil, ErrOAuthClientNameRequired
	}
	if len(app.RedirectURIs) == 0 {
		return nil, ErrOAuthClientURIsRequired
	}
	if err := s.repo.Update(ctx, app); err != nil {
		return nil, err
	}
	return app, nil
}

func (s *OAuthClientAppService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *OAuthClientAppService) List(ctx context.Context) ([]*OAuthClientApp, error) {
	return s.repo.List(ctx)
}

// GetEnabledByClientID 授权端点用：客户端必须存在且启用。
func (s *OAuthClientAppService) GetEnabledByClientID(ctx context.Context, clientID string) (*OAuthClientApp, error) {
	app, err := s.repo.GetByClientID(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if !app.Enabled {
		return nil, ErrOAuthClientDisabled
	}
	return app, nil
}

// GetByClientID 令牌端点用：需要拿到密钥做比对（含停用状态以便给出明确报错）。
func (s *OAuthClientAppService) GetByClientID(ctx context.Context, clientID string) (*OAuthClientApp, error) {
	return s.repo.GetByClientID(ctx, clientID)
}

func (s *OAuthClientAppService) validateAndFill(name, clientID, clientSecret, redirectURIs string, allowLocalhost bool, remark string) (*OAuthClientApp, error) {
	app := &OAuthClientApp{
		Name:           strings.TrimSpace(name),
		ClientID:       strings.TrimSpace(clientID),
		RedirectURIs:   domain.NormalizeRedirectURIs(redirectURIs),
		AllowLocalhost: allowLocalhost,
		Enabled:        true,
		Remark:         strings.TrimSpace(remark),
	}
	if app.Name == "" {
		return nil, ErrOAuthClientNameRequired
	}
	if !oauthClientIDPattern.MatchString(app.ClientID) {
		return nil, ErrOAuthClientIDInvalid
	}
	if len(app.RedirectURIs) == 0 {
		return nil, ErrOAuthClientURIsRequired
	}
	if clientSecret = strings.TrimSpace(clientSecret); clientSecret != "" {
		if len(clientSecret) < 16 || len(clientSecret) > 128 {
			return nil, ErrOAuthClientSecretInvalid
		}
		app.ClientSecret = clientSecret
	} else {
		secret, err := generateOAuthClientSecret()
		if err != nil {
			return nil, err
		}
		app.ClientSecret = secret
	}
	return app, nil
}

func generateOAuthClientSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate oauth client secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
