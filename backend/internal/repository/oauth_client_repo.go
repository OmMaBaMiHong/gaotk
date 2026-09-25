package repository

import (
	"context"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/oauthclient"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type oauthClientAppRepository struct {
	client *dbent.Client
}

func NewOAuthClientAppRepository(client *dbent.Client) service.OAuthClientAppRepository {
	return &oauthClientAppRepository{client: client}
}

func (r *oauthClientAppRepository) Create(ctx context.Context, app *service.OAuthClientApp) error {
	client := clientFromContext(ctx, r.client)
	created, err := client.OAuthClient.Create().
		SetName(app.Name).
		SetClientID(app.ClientID).
		SetClientSecret(app.ClientSecret).
		SetRedirectUris(strings.Join(app.RedirectURIs, "\n")).
		SetAllowLocalhost(app.AllowLocalhost).
		SetEnabled(app.Enabled).
		SetRemark(app.Remark).
		Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrOAuthClientNotFound, service.ErrOAuthClientIDDuplicated)
	}
	applyOAuthClientEntityToService(app, created)
	return nil
}

func (r *oauthClientAppRepository) GetByID(ctx context.Context, id int64) (*service.OAuthClientApp, error) {
	client := clientFromContext(ctx, r.client)
	entity, err := client.OAuthClient.Query().
		Where(oauthclient.IDEQ(id)).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrOAuthClientNotFound, nil)
	}
	app := &service.OAuthClientApp{}
	applyOAuthClientEntityToService(app, entity)
	return app, nil
}

func (r *oauthClientAppRepository) GetByClientID(ctx context.Context, clientID string) (*service.OAuthClientApp, error) {
	client := clientFromContext(ctx, r.client)
	entity, err := client.OAuthClient.Query().
		Where(oauthclient.ClientIDEQ(clientID)).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrOAuthClientNotFound, nil)
	}
	app := &service.OAuthClientApp{}
	applyOAuthClientEntityToService(app, entity)
	return app, nil
}

func (r *oauthClientAppRepository) Update(ctx context.Context, app *service.OAuthClientApp) error {
	client := clientFromContext(ctx, r.client)
	builder := client.OAuthClient.UpdateOneID(app.ID).
		SetName(app.Name).
		SetClientID(app.ClientID).
		SetClientSecret(app.ClientSecret).
		SetRedirectUris(strings.Join(app.RedirectURIs, "\n")).
		SetAllowLocalhost(app.AllowLocalhost).
		SetEnabled(app.Enabled).
		SetRemark(app.Remark)
	updated, err := builder.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrOAuthClientNotFound, service.ErrOAuthClientIDDuplicated)
	}
	applyOAuthClientEntityToService(app, updated)
	return nil
}

func (r *oauthClientAppRepository) Delete(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	err := client.OAuthClient.DeleteOneID(id).Exec(ctx)
	return translatePersistenceError(err, service.ErrOAuthClientNotFound, nil)
}

func (r *oauthClientAppRepository) List(ctx context.Context) ([]*service.OAuthClientApp, error) {
	client := clientFromContext(ctx, r.client)
	entities, err := client.OAuthClient.Query().
		Order(dbent.Desc(oauthclient.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	apps := make([]*service.OAuthClientApp, 0, len(entities))
	for _, entity := range entities {
		app := &service.OAuthClientApp{}
		applyOAuthClientEntityToService(app, entity)
		apps = append(apps, app)
	}
	return apps, nil
}

func (r *oauthClientAppRepository) Count(ctx context.Context) (int, error) {
	client := clientFromContext(ctx, r.client)
	return client.OAuthClient.Query().Count(ctx)
}

func applyOAuthClientEntityToService(app *service.OAuthClientApp, entity *dbent.OAuthClient) {
	app.ID = entity.ID
	app.Name = entity.Name
	app.ClientID = entity.ClientID
	app.ClientSecret = entity.ClientSecret
	app.RedirectURIs = service.NormalizeRedirectURIs(entity.RedirectUris)
	app.AllowLocalhost = entity.AllowLocalhost
	app.Enabled = entity.Enabled
	app.Remark = entity.Remark
	app.CreatedAt = entity.CreatedAt
	app.UpdatedAt = entity.UpdatedAt
}
