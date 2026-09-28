package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/checkinrecord"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type checkinRecordRepository struct {
	client *dbent.Client
}

// NewCheckinRecordRepository 创建签到记录仓储。
// 所有方法经 clientFromContext 取 client：service 层把 ent tx 放进 ctx 后，
// 插入记录与入账自动落到同一事务，满足"发记录 + 加余额"的原子性要求。
func NewCheckinRecordRepository(client *dbent.Client) service.CheckinRecordRepository {
	return &checkinRecordRepository{client: client}
}

func (r *checkinRecordRepository) Create(ctx context.Context, record *service.CheckinRecord) error {
	created, err := clientFromContext(ctx, r.client).CheckinRecord.Create().
		SetUserID(record.UserID).
		SetCheckinDate(record.CheckinDate).
		SetAmountAwarded(record.AmountAwarded).
		SetStreakDays(record.StreakDays).
		Save(ctx)
	if err != nil {
		// 唯一约束 (user_id, checkin_date) 冲突 = 今日已签到，这是并发双击的正确出口。
		return translatePersistenceError(err, nil, service.ErrCheckinAlreadyDone)
	}
	record.ID = created.ID
	record.CreatedAt = created.CreatedAt
	return nil
}

func (r *checkinRecordRepository) GetByUserAndDate(ctx context.Context, userID int64, date string) (*service.CheckinRecord, error) {
	found, err := clientFromContext(ctx, r.client).CheckinRecord.Query().
		Where(
			checkinrecord.UserIDEQ(userID),
			checkinrecord.CheckinDateEQ(date),
		).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrCheckinNotFound, nil)
	}
	return checkinRecordToService(found), nil
}

func (r *checkinRecordRepository) ListByUserAndDateRange(ctx context.Context, userID int64, fromDate, toDate string) ([]service.CheckinRecord, error) {
	records, err := clientFromContext(ctx, r.client).CheckinRecord.Query().
		Where(
			checkinrecord.UserIDEQ(userID),
			checkinrecord.CheckinDateGTE(fromDate),
			checkinrecord.CheckinDateLTE(toDate),
		).
		Order(checkinrecord.ByCheckinDate()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.CheckinRecord, 0, len(records))
	for _, record := range records {
		out = append(out, *checkinRecordToService(record))
	}
	return out, nil
}

func (r *checkinRecordRepository) StatsByUser(ctx context.Context, userID int64) (int64, float64, error) {
	// 简单聚合走原生 SQL，避免为一次 COUNT+SUM 拉全量行。
	const statsSQL = `
		SELECT COUNT(*), COALESCE(SUM(amount_awarded), 0)
		FROM checkin_records
		WHERE user_id = $1
	`
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, statsSQL, userID)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	if !rows.Next() {
		if rowsErr := rows.Err(); rowsErr != nil {
			return 0, 0, rowsErr
		}
		return 0, 0, nil
	}
	var totalCount int64
	var totalAmount float64
	if err := rows.Scan(&totalCount, &totalAmount); err != nil {
		return 0, 0, err
	}
	return totalCount, totalAmount, rows.Err()
}

// CreditUserBalance 入账方向的原子加钱：照 AdjustBalance 的"读与写压进同一条
// UPDATE + RETURNING"写法，但去掉 >=0 防负守卫（那是扣款方向的语义，签到是
// 入账，余额为负的透支用户也应能收到签到奖励），也不累加 total_recharged
// （那是充值口径统计，签到奖励混入会污染充值相关通知的百分比计算）。
func (r *checkinRecordRepository) CreditUserBalance(ctx context.Context, userID int64, delta float64) (service.BalanceChange, error) {
	const updateSQL = `
		UPDATE users
		SET balance = balance + $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING balance - $1, balance
	`
	change, ok, err := scanBalanceChange(ctx, clientFromContext(ctx, r.client), updateSQL, delta, userID)
	if err != nil {
		return service.BalanceChange{}, err
	}
	if !ok {
		return service.BalanceChange{}, service.ErrUserNotFound
	}
	return change, nil
}

func checkinRecordToService(record *dbent.CheckinRecord) *service.CheckinRecord {
	return &service.CheckinRecord{
		ID:            record.ID,
		UserID:        record.UserID,
		CheckinDate:   record.CheckinDate,
		AmountAwarded: record.AmountAwarded,
		StreakDays:    record.StreakDays,
		CreatedAt:     record.CreatedAt,
	}
}
