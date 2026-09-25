package service

import (
	"errors"
	"math"

	"github.com/shopspring/decimal"
)

// RentalSnapshot is captured with the scheduled account, never reconstructed
// from a later policy when settling an in-flight request.
type RentalSnapshot struct {
	OwnerAccountID      int64    `json:"owner_account_id"`
	OwnerUserID         int64    `json:"owner_user_id"`
	AdminUserID         int64    `json:"admin_user_id"`
	ChannelID           int64    `json:"channel_id"`
	OwnerShareBPS       int      `json:"owner_share_bps"`
	Platform            string   `json:"platform"`
	GroupID             int64    `json:"group_id"`
	Enabled             bool     `json:"enabled"`
	Status              string   `json:"status"`
	Schedulable         bool     `json:"schedulable"`
	OwnerAccountType    string   `json:"owner_account_type"`
	AllowedAccountTypes []string `json:"allowed_account_types"`
	OwnerVerifiedPlan   string   `json:"owner_verified_plan,omitempty"`
	OwnerCurrentPlan    string   `json:"owner_current_plan,omitempty"`
	AllowedPlans        []string `json:"allowed_plans,omitempty"`
	// Channels is keyed by the actual request group, never by a default channel.
	Channels map[int64]*RentalSnapshot `json:"channels,omitempty"`
}

func (a *Account) hasVerifiedSavingsPlan() bool {
	if a.OwnerUserID == nil && a.Rental == nil {
		return true
	}
	if a.Rental == nil || a.Rental.OwnerAccountType != a.Type || a.Rental.Platform != a.Platform {
		return false
	}
	if a.Type == AccountTypeAPIKey {
		return true
	}
	if a.Type != AccountTypeOAuth || !IsTokenSavingsPlanForPlatform(a.Platform, a.Rental.OwnerVerifiedPlan) || a.Rental.OwnerVerifiedPlan != a.Rental.OwnerCurrentPlan {
		return false
	}
	if a.ParentAccountID == nil && (a.GetCredential("savings_verified_plan_type") != a.Rental.OwnerVerifiedPlan || a.GetCredential("plan_type") != a.Rental.OwnerVerifiedPlan) {
		return false
	}
	return true
}

// IsTokenSavingsSchedulableForGroup checks the actual request group; a plan
// accepted by another linked group never grants access to this group's pool.
func (a *Account) IsTokenSavingsSchedulableForGroup(groupID *int64) bool {
	if a == nil {
		return false
	}
	if a.OwnerUserID == nil && a.Rental == nil {
		return true
	}
	if !a.hasVerifiedSavingsPlan() || groupID == nil {
		return false
	}
	selected := a.Rental.ForGroup(*groupID)
	if selected == nil {
		return false
	}
	rule := &SavingsReceivingRule{AccountTypes: selected.AllowedAccountTypes, AllowedPlans: selected.AllowedPlans}
	// Snapshots always capture explicit types. Missing fields from old caches fail closed.
	if selected.AllowedAccountTypes == nil {
		return false
	}
	return rule.AllowsAccount(a.Rental.Platform, a.Rental.OwnerAccountType, a.Rental.OwnerVerifiedPlan)
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
