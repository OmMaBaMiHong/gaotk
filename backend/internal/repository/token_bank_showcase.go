package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Showcase reads only committed owner credits from the settlement ledger.
// Names remain internal here; TokenBankShowcaseService masks them before use.
func (r *tokenBankRepository) Showcase(ctx context.Context) (*service.TokenBankShowcase, error) {
	out := &service.TokenBankShowcase{Leaderboard: []service.TokenBankLeader{}, Recent: []service.TokenBankRecent{}}
	rows, err := r.db.QueryContext(ctx, `SELECT u.username,SUM(l.owner_amount) AS amount
	FROM account_revenue_ledger l JOIN users u ON u.id=l.owner_user_id AND u.deleted_at IS NULL
	WHERE l.owner_amount>0 GROUP BY u.id,u.username
	ORDER BY amount DESC,u.id ASC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row service.TokenBankLeader
		if err := rows.Scan(&row.Name, &row.Amount); err != nil {
			return nil, err
		}
		row.Rank = len(out.Leaderboard) + 1
		out.Leaderboard = append(out.Leaderboard, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = r.db.QueryContext(ctx, `SELECT u.username,l.platform,l.owner_amount,l.created_at
	FROM account_revenue_ledger l JOIN users u ON u.id=l.owner_user_id AND u.deleted_at IS NULL
	WHERE l.owner_amount>0 ORDER BY l.id DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row service.TokenBankRecent
		if err := rows.Scan(&row.Name, &row.Platform, &row.Amount, &row.CreatedAt); err != nil {
			return nil, err
		}
		out.Recent = append(out.Recent, row)
	}
	return out, rows.Err()
}
