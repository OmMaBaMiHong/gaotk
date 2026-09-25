//go:build unit

package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRevenueShareStrategy(t *testing.T) {
	strategy := ProportionalRevenueShare{}
	for _, tc := range []struct {
		amount       float64
		bps          int
		owner, admin string
	}{
		{1, 8000, "0.80000000", "0.20000000"},
		{0.00000001, 8000, "0.00000001", "0.00000000"},
		{0.00000005, 5000, "0.00000003", "0.00000002"},
		{0.000078125, 8000, "0.00006250", "0.00001563"},
		{0, 8000, "0.00000000", "0.00000000"},
		{1, 0, "0.00000000", "1.00000000"},
		{1, 10000, "1.00000000", "0.00000000"},
	} {
		a, err := strategy.Split(tc.amount, RentalSnapshot{OwnerShareBPS: tc.bps})
		require.NoError(t, err)
		require.Equal(t, tc.owner, a.Owner.StringFixed(8))
		require.Equal(t, tc.admin, a.Admin.StringFixed(8))
		require.True(t, a.Owner.Add(a.Admin).Equal(a.Bill))
	}
	for _, amount := range []float64{-1, math.NaN(), math.Inf(1)} {
		_, err := strategy.Split(amount, RentalSnapshot{OwnerShareBPS: 8000})
		require.Error(t, err)
	}
	for _, bps := range []int{-1, 10001} {
		_, err := strategy.Split(1, RentalSnapshot{OwnerShareBPS: bps})
		require.Error(t, err)
	}
}

func TestTokenSavingsSelectsActualGroupWithoutChangingBilling(t *testing.T) {
	firstGroup, actualGroup := int64(11), int64(22)
	p := &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 10, TotalCost: 6},
		User: &User{ID: 1}, APIKey: &APIKey{ID: 2, GroupID: &firstGroup},
		Account: &Account{ID: 3, Type: AccountTypeAPIKey, Rental: &RentalSnapshot{Channels: map[int64]*RentalSnapshot{
			11: {ChannelID: 100, GroupID: 11, OwnerShareBPS: 3000, Enabled: true},
			22: {ChannelID: 200, GroupID: 22, OwnerShareBPS: 8000, Enabled: true},
		}}}, AccountRateMultiplier: 1,
	}
	fallback := buildUsageBillingCommand("request", nil, p)
	require.Equal(t, int64(100), fallback.Rental.ChannelID)
	cmd := buildUsageBillingCommand("request", &UsageLog{GroupID: &actualGroup}, p)
	require.Equal(t, int64(200), cmd.Rental.ChannelID)
	require.Equal(t, 10.0, cmd.BalanceCost)
	allocation, err := RentalRevenueStrategy().Split(cmd.BalanceCost, *cmd.Rental)
	require.NoError(t, err)
	require.Equal(t, "8.00000000", allocation.Owner.StringFixed(8))
	require.Equal(t, "2.00000000", allocation.Admin.StringFixed(8))
	withSavings := *cmd
	p.Account.Rental = nil
	withoutSavings := buildUsageBillingCommand("request", &UsageLog{GroupID: &actualGroup}, p)
	withSavings.Rental = nil
	withSavings.RequestFingerprint = ""
	withSavings.Normalize()
	require.Equal(t, withoutSavings, &withSavings, "sharing adds only a snapshot; all original billing calculations stay identical")
}

func TestTokenSavingsSnapshotSelectionAndFingerprint(t *testing.T) {
	snapshot := &RentalSnapshot{Channels: map[int64]*RentalSnapshot{
		1: {ChannelID: 10, GroupID: 1, OwnerShareBPS: 8000, Enabled: true},
		2: {ChannelID: 20, GroupID: 2, OwnerShareBPS: 4000, Enabled: false},
	}}
	require.Nil(t, snapshot.ForGroup(0))
	require.Nil(t, snapshot.ForGroup(2))
	require.Nil(t, snapshot.ForGroup(3))
	selected := snapshot.ForGroup(1)
	cmd := &UsageBillingCommand{Rental: selected}
	original := buildUsageBillingFingerprint(cmd)
	selected.OwnerShareBPS = 5000
	require.NotEqual(t, original, buildUsageBillingFingerprint(cmd))
	require.Equal(t, 8000, snapshot.Channels[1].OwnerShareBPS)
	selected.OwnerShareBPS = 8000
	selected.ChannelID = 11
	require.NotEqual(t, original, buildUsageBillingFingerprint(cmd))
}
