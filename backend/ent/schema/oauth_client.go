package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// OAuthClient holds the schema definition for the OAuthClient entity.
//
// 第三方应用接入中转站 OAuth 的客户端注册表（管理端 CRUD 维护）。
// 删除策略：硬删除（客户端注册信息无历史留存需求）。
type OAuthClient struct {
	ent.Schema
}

func (OAuthClient) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "oauth_clients"},
	}
}

func (OAuthClient) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			MaxLen(100).
			NotEmpty().
			Comment("应用名称（授权确认页展示给用户）"),
		field.String("client_id").
			MaxLen(64).
			NotEmpty().
			Unique().
			Comment("客户端 ID"),
		field.String("client_secret").
			MaxLen(128).
			NotEmpty().
			Comment("客户端密钥（管理端可重新生成）"),
		field.String("redirect_uris").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default("").
			Comment("回调地址白名单（换行分隔，精确匹配）"),
		field.Bool("allow_localhost").
			Default(false).
			Comment("是否放行 localhost/127.0.0.1/[::1] 任意端口回调（本地开发场景）"),
		field.Bool("enabled").
			Default(true).
			Comment("启用开关：关闭后该应用无法发起新授权/换 token"),
		field.String("remark").
			MaxLen(500).
			Default("").
			Comment("备注"),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}
