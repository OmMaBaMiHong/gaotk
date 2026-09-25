package service

import (
	"errors"
	"math"

	"github.com/shopspring/decimal"
)

// RentalSnapshot is captured with the scheduled account, never reconstructed
// from a later policy when settling an in-flight request.
type RentalSnapshot struct {
	OwnerAccountID int64  `json:"owner_account_id"`
	OwnerUserID    int64  `json:"owner_user_id"`
	AdminUserID    int64  `json:"admin_user_id"`
	ChannelID      int64  `json:"channel_id"`
	OwnerShareBPS  int    `json:"owner_share_bps"`
	Platform       string `json:"platform"`
	GroupID        int64  `json:"group_id"`
	Enabled        bool   `json:"enabled"`
	Status         string `json:"status"`
	Schedulable    bool   `json:"schedulable"`
	// Channels is keyed by the actual request group, never by a default channel.
	Channels map[int64]*RentalSnapshot `json:"channels,omitempty"`
}

// ForGroup selects a captured channel policy without reloading mutable configuration.
func (s *RentalSnapshot) ForGroup(groupID int64) *RentalSnapshot {
	if s == nil || groupID <= 0 {
		return nil
	}
	selected := s.Channels[groupID]
	if selected == nil || !selected.Enabled {
		return nil
	}
	cp := *selected
	cp.Channels = nil
	return &cp
}

// RevenueShareStrategy only allocates an already billed amount. It must not
// calculate token prices, change quotas, or write balances.
type RevenueShareStrategy interface {
	Split(billedAmount float64, snapshot RentalSnapshot) (RevenueAllocation, error)
}

type RevenueAllocation struct {
	Bill  decimal.Decimal
	Owner decimal.Decimal
	Admin decimal.Decimal
}

// ProportionalRevenueShare allocates the billed amount; 8000 bps means 80%.
type ProportionalRevenueShare struct{}

// RentalRevenueStrategy selects the current template in one place.
func RentalRevenueStrategy() RevenueShareStrategy { return ProportionalRevenueShare{} }

func (ProportionalRevenueShare) Split(amount float64, snapshot RentalSnapshot) (RevenueAllocation, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 || snapshot.OwnerShareBPS < 0 || snapshot.OwnerShareBPS > 10000 {
		return RevenueAllocation{}, errors.New("invalid rental revenue amount or ratio")
	}
	bill := decimal.NewFromFloat(amount).Round(UsageBillingMonetaryScale)
	owner := bill.Mul(decimal.NewFromInt(int64(snapshot.OwnerShareBPS))).Div(decimal.NewFromInt(10000)).Round(UsageBillingMonetaryScale)
	return RevenueAllocation{Bill: bill, Owner: owner, Admin: bill.Sub(owner)}, nil
}
