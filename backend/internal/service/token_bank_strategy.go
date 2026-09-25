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
	PolicyID       int64  `json:"policy_id"`
	PolicyVersion  int    `json:"policy_version"`
	OwnerShareBPS  int    `json:"owner_share_bps"`
	Platform       string `json:"platform"`
	GroupID        int64  `json:"group_id"`
	Enabled        bool   `json:"enabled"`
	Status         string `json:"status"`
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

// ProportionalRevenueShare is the platform policy template; 8000 bps means 80%.
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
