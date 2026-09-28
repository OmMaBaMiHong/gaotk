package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 签到业务错误。重复签到走 Conflict（唯一约束冲突是并发场景下的最终防线）。
var (
	ErrCheckinDisabled    = infraerrors.BadRequest("CHECKIN_DISABLED", "check-in is disabled")
	ErrCheckinAlreadyDone = infraerrors.Conflict("CHECKIN_ALREADY_DONE", "already checked in today")
	ErrCheckinNotFound    = infraerrors.NotFound("CHECKIN_RECORD_NOT_FOUND", "check-in record not found")
)

// checkinDateLayout 服务器本地日期口径（schema 注释与 checkin_records.checkin_date 同一格式）。
const checkinDateLayout = "2006-01-02"

// CheckinRecord 签到发放记录（服务层模型，与 checkin_records 表行一一对应）。
type CheckinRecord struct {
	ID            int64
	UserID        int64
	CheckinDate   string    // YYYY-MM-DD
	AmountAwarded float64   // 本次发放的美元额度
	StreakDays    int       // 连续签到天数（含当天）
	CreatedAt     time.Time // 发放时间（记录的是 created_at 而非签到日，跨日补看以 CheckinDate 为准）
}

// CheckinRecordRepository 签到记录仓储。实现方必须尊重 ctx 中携带的事务
// （dbent.NewTxContext），否则插入记录与加余额无法落入同一事务。
type CheckinRecordRepository interface {
	// Create 插入签到记录；(user_id, checkin_date) 唯一约束冲突时返回 ErrCheckinAlreadyDone。
	Create(ctx context.Context, record *CheckinRecord) error
	// GetByUserAndDate 精确取某一天的签到记录，没有则返回 ErrCheckinNotFound。
	GetByUserAndDate(ctx context.Context, userID int64, date string) (*CheckinRecord, error)
	// ListByUserAndDateRange 返回 [from, to] 闭区间内的签到记录（按日期升序）。
	ListByUserAndDateRange(ctx context.Context, userID int64, fromDate, toDate string) ([]CheckinRecord, error)
	// StatsByUser 返回累计签到次数与累计发放金额。
	StatsByUser(ctx context.Context, userID int64) (totalCount int64, totalAmount float64, err error)
	// CreditUserBalance 原子入账（无防负守卫、不计入累计充值），返回变更前后余额。
	// 与 Create 同一事务上下文调用，保证"发记录"与"加余额"同生共死。
	CreditUserBalance(ctx context.Context, userID int64, delta float64) (BalanceChange, error)
}

// CheckinSettingsReader 抽象签到配置读取，便于单测替换 *SettingService。
type CheckinSettingsReader interface {
	IsCheckinEnabled(ctx context.Context) bool
	GetCheckinMinAmount(ctx context.Context) float64
	GetCheckinMaxAmount(ctx context.Context) float64
	GetCheckinStreakBonusAmount(ctx context.Context) float64
}

// CheckinService 每日签到发美元额度。
type CheckinService struct {
	checkinRepo          CheckinRecordRepository
	settings             CheckinSettingsReader
	entClient            *dbent.Client
	billingCacheService  *BillingCacheService
	authCacheInvalidator APIKeyAuthCacheInvalidator
	random               func() float64   // [0,1) 随机源，测试可注入固定值
	now                  func() time.Time // 时钟，测试可注入固定时间
}

// NewCheckinService 创建签到服务。
func NewCheckinService(
	checkinRepo CheckinRecordRepository,
	settings CheckinSettingsReader,
	entClient *dbent.Client,
	billingCacheService *BillingCacheService,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
) *CheckinService {
	return &CheckinService{
		checkinRepo:          checkinRepo,
		settings:             settings,
		entClient:            entClient,
		billingCacheService:  billingCacheService,
		authCacheInvalidator: authCacheInvalidator,
		random:               rand.Float64,
		now:                  time.Now,
	}
}

// CheckinResult 签到成功后的发放结果。
type CheckinResult struct {
	AmountAwarded      float64 `json:"amount_awarded"`       // 本次发放总额（含加成）
	BaseAmount         float64 `json:"base_amount"`          // 随机基础金额
	StreakBonusApplied bool    `json:"streak_bonus_applied"` // 是否命中满 7 天加成
	StreakBonusAmount  float64 `json:"streak_bonus_amount"`  // 加成金额（未命中为 0）
	StreakDays         int     `json:"streak_days"`          // 连续签到天数（含当天）
	NewBalance         float64 `json:"new_balance"`          // 入账后的最新余额
}

// Checkin 执行签到：发一条签到记录 + 给余额加钱，两者同一事务。
//
// 并发正确性依赖 checkin_records (user_id, checkin_date) 唯一约束：并发双击时
// 后到的 INSERT 违反唯一索引而失败，事务整体回滚，余额不会被多发。
func (s *CheckinService) Checkin(ctx context.Context, userID int64) (*CheckinResult, error) {
	if !s.settings.IsCheckinEnabled(ctx) {
		return nil, ErrCheckinDisabled
	}

	now := s.now()
	today := now.Format(checkinDateLayout)

	// 快路径防呆：今日已签直接拒绝，省一次事务开销。真正的并发防线在唯一约束。
	if _, err := s.checkinRepo.GetByUserAndDate(ctx, userID, today); err == nil {
		return nil, ErrCheckinAlreadyDone
	} else if !errors.Is(err, ErrCheckinNotFound) {
		return nil, fmt.Errorf("get today checkin record: %w", err)
	}

	// 连续天数：昨天有记录则在其基础上 +1，否则重置为 1（按老板拍板的 new-api 口径）。
	// 计算收口在纯函数 computeStreakDays，单测直接覆盖该函数。
	yesterday := now.AddDate(0, 0, -1).Format(checkinDateLayout)
	streak := 1
	yesterdayRecord, err := s.checkinRepo.GetByUserAndDate(ctx, userID, yesterday)
	switch {
	case err == nil:
		streak = computeStreakDays(yesterdayRecord)
	case errors.Is(err, ErrCheckinNotFound):
		// 昨天没签，连续天数从今天重新计。
	default:
		return nil, fmt.Errorf("get yesterday checkin record: %w", err)
	}

	minAmount := s.settings.GetCheckinMinAmount(ctx)
	maxAmount := s.settings.GetCheckinMaxAmount(ctx)
	bonus := s.settings.GetCheckinStreakBonusAmount(ctx)
	baseAmount, bonusApplied := computeCheckinReward(minAmount, maxAmount, bonus, streak, s.random())
	bonusHit := bonusAppliedAmount(bonus, bonusApplied)
	amount := roundCheckinAmount(baseAmount)

	record := &CheckinRecord{
		UserID:        userID,
		CheckinDate:   today,
		AmountAwarded: amount,
		StreakDays:    streak,
	}

	// 插入记录与加余额必须同一事务：任何一步失败整体回滚。
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin checkin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)

	// 唯一约束是并发重复签到的最终防线：冲突即"今日已签到"。
	if err := s.checkinRepo.Create(txCtx, record); err != nil {
		if errors.Is(err, ErrCheckinAlreadyDone) {
			return nil, ErrCheckinAlreadyDone
		}
		return nil, fmt.Errorf("create checkin record: %w", err)
	}

	change, err := s.checkinRepo.CreditUserBalance(txCtx, userID, amount)
	if err != nil {
		return nil, fmt.Errorf("credit checkin balance: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit checkin transaction: %w", err)
	}

	// 事务提交后再失效缓存（best-effort），避免回滚路径误删缓存。
	s.invalidateBalanceCaches(ctx, userID)

	return &CheckinResult{
		AmountAwarded:      amount,
		BaseAmount:         roundCheckinAmount(amount - bonusHit),
		StreakBonusApplied: bonusApplied,
		StreakBonusAmount:  bonusHit,
		StreakDays:         streak,
		NewBalance:         change.New,
	}, nil
}

// CheckinDay 本月签到日历的单日条目。
type CheckinDay struct {
	Date       string  `json:"date"`        // YYYY-MM-DD
	Amount     float64 `json:"amount"`      // 当日发放金额
	StreakDays int     `json:"streak_days"` // 当日连续天数
}

// CheckinStatus 用户签到面板数据。
type CheckinStatus struct {
	Enabled            bool         `json:"enabled"`
	TodayCheckedIn     bool         `json:"today_checked_in"`
	TodayAmount        float64      `json:"today_amount"`
	StreakDays         int          `json:"streak_days"`
	MinAmount          float64      `json:"min_amount"`
	MaxAmount          float64      `json:"max_amount"`
	StreakBonusAmount  float64      `json:"streak_bonus_amount"`
	StreakBonusDays    int          `json:"streak_bonus_days"`
	MonthRecords       []CheckinDay `json:"month_records"`
	TotalCheckinCount  int64        `json:"total_checkin_count"`
	TotalAwardedAmount float64      `json:"total_awarded_amount"`
}

// checkinStreakLookbackDays 计算当前连续天数时的回看窗口上限。
// 一条查询取回窗口内记录再本地游走，避免按天逐条查库；超过窗口的超长连续
// 在面板上会被截断显示，属可接受的展示边界。
const checkinStreakLookbackDays = 400

// Status 返回签到面板数据：开关、今日状态、连续天数、本月日历与累计统计。
func (s *CheckinService) Status(ctx context.Context, userID int64) (*CheckinStatus, error) {
	enabled := s.settings.IsCheckinEnabled(ctx)
	minAmount := s.settings.GetCheckinMinAmount(ctx)
	maxAmount := s.settings.GetCheckinMaxAmount(ctx)
	bonus := s.settings.GetCheckinStreakBonusAmount(ctx)

	now := s.now()
	today := now.Format(checkinDateLayout)

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format(checkinDateLayout)
	monthRecords, err := s.checkinRepo.ListByUserAndDateRange(ctx, userID, monthStart, today)
	if err != nil {
		return nil, fmt.Errorf("list month checkin records: %w", err)
	}

	days := make([]CheckinDay, 0, len(monthRecords))
	checkedDates := make(map[string]struct{}, len(monthRecords))
	todayAmount := 0.0
	for i := range monthRecords {
		record := &monthRecords[i]
		days = append(days, CheckinDay{
			Date:       record.CheckinDate,
			Amount:     record.AmountAwarded,
			StreakDays: record.StreakDays,
		})
		checkedDates[record.CheckinDate] = struct{}{}
		if record.CheckinDate == today {
			todayAmount = record.AmountAwarded
		}
	}

	// 当前连续天数从窗口起点一次性取数后本地游走：今天已签从今天往回数，
	// 否则从昨天往回数（今天没签不断掉已有的连续）。
	lookbackStart := now.AddDate(0, 0, -checkinStreakLookbackDays).Format(checkinDateLayout)
	recentRecords, err := s.checkinRepo.ListByUserAndDateRange(ctx, userID, lookbackStart, today)
	if err != nil {
		return nil, fmt.Errorf("list recent checkin records: %w", err)
	}
	recentDates := make(map[string]struct{}, len(recentRecords))
	for i := range recentRecords {
		recentDates[recentRecords[i].CheckinDate] = struct{}{}
	}

	totalCount, totalAmount, err := s.checkinRepo.StatsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get checkin stats: %w", err)
	}

	return &CheckinStatus{
		Enabled:            enabled,
		TodayCheckedIn:     todayAmount > 0 || containsDate(checkedDates, today),
		TodayAmount:        todayAmount,
		StreakDays:         computeCurrentStreakDays(recentDates, now),
		MinAmount:          minAmount,
		MaxAmount:          maxAmount,
		StreakBonusAmount:  bonus,
		StreakBonusDays:    CheckinStreakBonusInterval,
		MonthRecords:       days,
		TotalCheckinCount:  totalCount,
		TotalAwardedAmount: totalAmount,
	}, nil
}

// invalidateBalanceCaches 提交成功后失效余额相关缓存（与 redeem 入账路径同一做法，best-effort）。
func (s *CheckinService) invalidateBalanceCaches(ctx context.Context, userID int64) {
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCacheService == nil {
		return
	}
	go func() {
		cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.billingCacheService.InvalidateUserBalance(cacheCtx, userID)
	}()
}

// computeStreakDays 依据昨天的记录计算今天的连续签到天数。
// 纯函数：昨天有记录则在其 streak 上 +1，否则重置为 1。
func computeStreakDays(yesterdayRecord *CheckinRecord) int {
	if yesterdayRecord == nil {
		return 1
	}
	if yesterdayRecord.StreakDays < 1 {
		// 脏数据防御：streak 不可能小于 1，出现时按"昨天刚签"处理而不是放大错误。
		return 2
	}
	return yesterdayRecord.StreakDays + 1
}

// computeCheckinReward 计算本次签到的随机基础金额与是否命中连续加成。
// 纯函数：random 为 [0,1) 随机数；min > max 时交换（防御误配置）；bonus 仅在
// streak 为 CheckinStreakBonusInterval 整数倍且 > 0 时叠加。
func computeCheckinReward(minAmount, maxAmount, streakBonus float64, streakDays int, random float64) (baseAmount float64, bonusApplied bool) {
	if minAmount > maxAmount {
		minAmount, maxAmount = maxAmount, minAmount
	}
	baseAmount = minAmount + random*(maxAmount-minAmount)
	if streakBonus > 0 && streakDays > 0 && streakDays%CheckinStreakBonusInterval == 0 {
		return baseAmount + streakBonus, true
	}
	return baseAmount, false
}

// bonusAppliedAmount 返回本次命中的加成金额，未命中为 0。
func bonusAppliedAmount(streakBonus float64, bonusApplied bool) float64 {
	if !bonusApplied {
		return 0
	}
	return streakBonus
}

// roundCheckinAmount 金额按 numeric(20,8) 精度取整，避免随机浮点把脏尾数写进流水。
func roundCheckinAmount(value float64) float64 {
	return math.Round(value*1e8) / 1e8
}

// computeCurrentStreakDays 从记录日期集合中游走出"今天（或昨天）往前"的连续签到天数。
// 纯函数：today 为锚点日期；今天没签不断掉连续（从昨天起算）。
func computeCurrentStreakDays(checkedDates map[string]struct{}, now time.Time) int {
	cursor := now
	if _, ok := checkedDates[cursor.Format(checkinDateLayout)]; !ok {
		cursor = cursor.AddDate(0, 0, -1)
	}
	streak := 0
	for ; streak < checkinStreakLookbackDays+1; streak++ {
		if _, ok := checkedDates[cursor.Format(checkinDateLayout)]; !ok {
			break
		}
		cursor = cursor.AddDate(0, 0, -1)
	}
	return streak
}

// containsDate 小工具：集合里是否包含该日期。
func containsDate(dates map[string]struct{}, date string) bool {
	_, ok := dates[date]
	return ok
}
