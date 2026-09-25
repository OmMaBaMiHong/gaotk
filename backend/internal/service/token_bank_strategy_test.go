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
