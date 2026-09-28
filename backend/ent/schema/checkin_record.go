package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CheckinRecord 每日签到发放记录。
//
// (user_id, checkin_date) 唯一约束是防并发重复签到的唯一正确性来源：
// 并发双击时后到的 INSERT 违反唯一索引而失败，事务整体回滚，余额不会被多发。
// 发放金额与 users.balance 同口径（美元额度），本表即签到的发放流水。
type CheckinRecord struct {
	ent.Schema
}

func (CheckinRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "checkin_records"},
	}
}

func (CheckinRecord) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id").
			Comment("用户 ID"),
		field.String("checkin_date").
			MaxLen(10).
			Comment("签到日，服务器本地日期 YYYY-MM-DD"),
		field.Float("amount_awarded").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Comment("本次发放的美元额度（users.balance 同口径）"),
		field.Int("streak_days").
			Default(1).
			Comment("连续签到天数（含当天）"),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CheckinRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "checkin_date").Unique(),
		index.Fields("user_id", "created_at"),
	}
}
