//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeCheckinSettings 签到配置桩：固定返回构造时给定的四个配置值。
type fakeCheckinSettings struct {
	enabled bool
	min     float64
	max     float64
	bonus   float64
}

func (f *fakeCheckinSettings) IsCheckinEnabled(context.Context) bool       { return f.enabled }
func (f *fakeCheckinSettings) GetCheckinMinAmount(context.Context) float64 { return f.min }
func (f *fakeCheckinSettings) GetCheckinMaxAmount(context.Context) float64 { return f.max }
func (f *fakeCheckinSettings) GetCheckinStreakBonusAmount(context.Context) float64 {
	return f.bonus
}

// fakeCheckinRepo 签到仓储桩：只服务"重复签到拒绝"的快路径测试，
// 记录 Create 调用以便断言事务从未开启（entClient 为 nil 时触库即 panic）。
type fakeCheckinRepo struct {
	byDate    map[string]*CheckinRecord // key: checkin_date
	created   []*CheckinRecord
	listRange []CheckinRecord
}

func (f *fakeCheckinRepo) Create(_ context.Context, record *CheckinRecord) error {
	f.created = append(f.created, record)
	return nil
}

func (f *fakeCheckinRepo) GetByUserAndDate(_ context.Context, _ int64, date string) (*CheckinRecord, error) {
	if record, ok := f.byDate[date]; ok {
		return record, nil
	}
	return nil, ErrCheckinNotFound
}

func (f *fakeCheckinRepo) ListByUserAndDateRange(_ context.Context, _ int64, _, _ string) ([]CheckinRecord, error) {
	return f.listRange, nil
}

func (f *fakeCheckinRepo) StatsByUser(_ context.Context, _ int64) (int64, float64, error) {
	return 0, 0, nil
}

func (f *fakeCheckinRepo) CreditUserBalance(_ context.Context, _ int64, delta float64) (BalanceChange, error) {
	return BalanceChange{Old: 1, New: 1 + delta}, nil
}

func TestComputeStreakDaysWithYesterdayRecord(t *testing.T) {
	streak := computeStreakDays(&CheckinRecord{StreakDays: 6})
	require.Equal(t, 7, streak)
}

func TestComputeStreakDaysWithoutYesterdayRecord(t *testing.T) {
	require.Equal(t, 1, computeStreakDays(nil))
}

func TestComputeStreakDaysDirtyStreakGuard(t *testing.T) {
	// streak 字段不可能小于 1，脏数据出现时按"昨天刚签"处理而不是放大错误。
	require.Equal(t, 2, computeStreakDays(&CheckinRecord{StreakDays: 0}))
	require.Equal(t, 2, computeStreakDays(&CheckinRecord{StreakDays: -3}))
}

func TestComputeCheckinRewardStaysInRange(t *testing.T) {
	const min, max = 0.01, 0.05
	for i := 0; i < 1000; i++ {
		random := float64(i%1000) / 1000 // 覆盖 [0,1) 的确定性采样
		amount, bonusApplied := computeCheckinReward(min, max, 0, 1, random)
		require.False(t, bonusApplied)
		require.GreaterOrEqual(t, amount, min)
		require.LessOrEqual(t, amount, max)
	}
}

func TestComputeCheckinRewardFixedWhenMinEqualsMax(t *testing.T) {
	amount, _ := computeCheckinReward(0.03, 0.03, 0, 1, 0.999)
	require.Equal(t, 0.03, amount)
}

func TestComputeCheckinRewardSwapsInvertedRange(t *testing.T) {
	// 管理员把 min/max 填反时按交换处理，而不是发出负数或越界奖励。
	amount, _ := computeCheckinReward(0.05, 0.01, 0, 1, 0.5)
	require.GreaterOrEqual(t, amount, 0.01)
	require.LessOrEqual(t, amount, 0.05)
}

func TestComputeCheckinRewardStreakBonusAtMultiplesOfSeven(t *testing.T) {
	amount7, applied7 := computeCheckinReward(0.01, 0.05, 0.05, 7, 0)
	require.True(t, applied7)
	require.InDelta(t, 0.06, amount7, 1e-9)

	amount14, applied14 := computeCheckinReward(0.01, 0.05, 0.05, 14, 0)
	require.True(t, applied14)
	require.InDelta(t, 0.06, amount14, 1e-9)

	// 未满 7 的倍数不加成。
	amount5, applied5 := computeCheckinReward(0.01, 0.05, 0.05, 5, 0)
	require.False(t, applied5)
	require.InDelta(t, 0.01, amount5, 1e-9)

	amount6, applied6 := computeCheckinReward(0.01, 0.05, 0.05, 6, 0)
	require.False(t, applied6)
	require.InDelta(t, 0.01, amount6, 1e-9)
}

func TestComputeCheckinRewardBonusDisabledWhenZero(t *testing.T) {
	_, applied := computeCheckinReward(0.01, 0.05, 0, 7, 0)
	require.False(t, applied)
}

func TestComputeCurrentStreakDays(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	today := now.Format(checkinDateLayout)

	// 今天没签：从昨天往回数，不断掉已有连续。
	dates := map[string]struct{}{}
	day := now.AddDate(0, 0, -1)
	for i := 0; i < 4; i++ {
		dates[day.Format(checkinDateLayout)] = struct{}{}
		day = day.AddDate(0, 0, -1)
	}
	require.Equal(t, 4, computeCurrentStreakDays(dates, now))

	// 今天已签：从今天起算。
	dates[today] = struct{}{}
	require.Equal(t, 5, computeCurrentStreakDays(dates, now))

	// 完全没签过。
	require.Equal(t, 0, computeCurrentStreakDays(map[string]struct{}{}, now))

	// 昨天断签：即使前天有记录也归零。
	broken := map[string]struct{}{now.AddDate(0, 0, -2).Format(checkinDateLayout): {}}
	require.Equal(t, 0, computeCurrentStreakDays(broken, now))
}

func newCheckinServiceForTest(repo CheckinRecordRepository, settings CheckinSettingsReader) *CheckinService {
	// entClient 为 nil：重复签到走快路径短路，绝不触库；若逻辑回归到触库路径会直接 panic 暴露问题。
	return NewCheckinService(repo, settings, nil, nil, nil)
}

func TestCheckinRejectsDuplicateSameDay(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	today := now.Format(checkinDateLayout)
	repo := &fakeCheckinRepo{byDate: map[string]*CheckinRecord{
		today: {UserID: 1, CheckinDate: today, AmountAwarded: 0.02, StreakDays: 3},
	}}
	svc := newCheckinServiceForTest(repo, &fakeCheckinSettings{enabled: true, min: 0.01, max: 0.05, bonus: 0.05})
	svc.now = func() time.Time { return now }

	result, err := svc.Checkin(context.Background(), 1)
	require.Nil(t, result)
	require.True(t, errors.Is(err, ErrCheckinAlreadyDone))
	require.Empty(t, repo.created, "重复签到不得再发一条记录")
}

func TestCheckinRejectsWhenDisabled(t *testing.T) {
	repo := &fakeCheckinRepo{byDate: map[string]*CheckinRecord{}}
	svc := newCheckinServiceForTest(repo, &fakeCheckinSettings{enabled: false})
	svc.now = func() time.Time { return time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC) }

	_, err := svc.Checkin(context.Background(), 1)
	require.True(t, errors.Is(err, ErrCheckinDisabled))
	require.Empty(t, repo.created)
}

func TestRoundCheckinAmountClipsFloatTail(t *testing.T) {
	require.Equal(t, 0.03, roundCheckinAmount(0.03))
	require.Equal(t, 0.06, roundCheckinAmount(0.01+0.05))
	require.True(t, math.Abs(roundCheckinAmount(0.1+0.2)-0.3) < 1e-12)
}
