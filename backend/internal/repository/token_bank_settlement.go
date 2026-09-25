package repository

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// rentalSettlement owns ledger and credit operations. The billing transaction
// calls it before/after its existing effects without knowing the split formula.
type rentalSettlement struct {
	snapshot   service.RentalSnapshot
	allocation service.RevenueAllocation
}

func prepareRentalSettlement(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (*rentalSettlement, error) {
	if cmd.Rental == nil {
		return nil, nil
	}
	s := *cmd.Rental
	if s.OwnerAccountID <= 0 || s.OwnerUserID <= 0 || s.AdminUserID <= 0 || s.ChannelID <= 0 || s.GroupID <= 0 {
		return nil, errors.New("incomplete rental settlement snapshot")
	}
	allocation, err := service.RentalRevenueStrategy().Split(cmd.BalanceCost+cmd.SubscriptionCost, s)
	if err != nil {
		return nil, err
	}
	if allocation.Bill.IsZero() {
		return nil, nil
	}
	// Lock all participants in the same order before the existing deduction.
	// This also covers a consumer renting their own account and reciprocal calls.
	ids := []int64{cmd.UserID, s.OwnerUserID, s.AdminUserID}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for i, id := range ids {
		if i > 0 && id == ids[i-1] {
			continue
		}
		var locked int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&locked); err != nil {
			return nil, err
		}
	}
	return &rentalSettlement{snapshot: s, allocation: allocation}, nil
}

func (s *rentalSettlement) apply(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	if s == nil {
		return nil
	}
	p, a := s.snapshot, s.allocation
	_, err := tx.ExecContext(ctx, `INSERT INTO account_revenue_ledger
	(request_id,api_key_id,account_id,owner_account_id,owner_user_id,admin_user_id,channel_id,platform,group_id,model,billing_type,input_tokens,output_tokens,cache_tokens,bill_amount,owner_share_bps,owner_amount,admin_amount)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		cmd.RequestID, cmd.APIKeyID, cmd.AccountID, p.OwnerAccountID, p.OwnerUserID, p.AdminUserID, p.ChannelID, p.Platform, p.GroupID, cmd.Model, cmd.BillingType, cmd.InputTokens, cmd.OutputTokens, cmd.CacheCreationTokens+cmd.CacheReadTokens, a.Bill.StringFixed(8), p.OwnerShareBPS, a.Owner.StringFixed(8), a.Admin.StringFixed(8))
	if err != nil {
		return err
	}
	for _, credit := range []struct {
		id     int64
		amount string
	}{{p.OwnerUserID, a.Owner.StringFixed(8)}, {p.AdminUserID, a.Admin.StringFixed(8)}} {
		var balance float64
		if err := tx.QueryRowContext(ctx, `UPDATE users SET balance=balance+$2::numeric,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL RETURNING balance`, credit.id, credit.amount).Scan(&balance); err != nil {
			return err
		}
		if credit.id == cmd.UserID {
			result.NewBalance = &balance
		}
	}
	result.RevenueUserIDs = []int64{p.OwnerUserID}
	if p.AdminUserID != p.OwnerUserID {
		result.RevenueUserIDs = append(result.RevenueUserIDs, p.AdminUserID)
	}
	return nil
}
