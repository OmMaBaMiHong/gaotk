package repository

import (
	"context"
	"database/sql"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type tokenBankRepository struct{ db *sql.DB }

func NewTokenBankRepository(db *sql.DB) service.TokenBankRepository {
	return &tokenBankRepository{db: db}
}

func (r *tokenBankRepository) Policies(ctx context.Context) ([]service.RentalPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,platform,group_id,owner_share_bps,admin_user_id,enabled,version FROM account_rental_policies ORDER BY platform`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.RentalPolicy{}
	for rows.Next() {
		var p service.RentalPolicy
		if err := rows.Scan(&p.ID, &p.Platform, &p.GroupID, &p.OwnerShareBPS, &p.AdminUserID, &p.Enabled, &p.Version); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *tokenBankRepository) SavePolicy(ctx context.Context, p service.RentalPolicy) error {
	if p.OwnerShareBPS < 0 || p.OwnerShareBPS > 10000 {
		return service.ErrRentalInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var groupID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM groups WHERE id=$1 AND platform=$2 AND status='active' AND deleted_at IS NULL FOR UPDATE`, p.GroupID, p.Platform).Scan(&groupID); err != nil {
		return service.ErrRentalInvalid
	}
	var adminID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND role='admin' AND status='active' AND deleted_at IS NULL FOR SHARE`, p.AdminUserID).Scan(&adminID); err != nil {
		return service.ErrRentalInvalid
	}
	// A pool cannot move while it owns accounts. A new platform starts disabled.
	var policyID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO account_rental_policies(platform,group_id,owner_share_bps,admin_user_id,enabled) VALUES($1,$2,$3,$4,$5)
	ON CONFLICT(platform) DO UPDATE SET group_id=EXCLUDED.group_id,owner_share_bps=EXCLUDED.owner_share_bps,admin_user_id=EXCLUDED.admin_user_id,enabled=EXCLUDED.enabled,version=account_rental_policies.version+1,updated_at=NOW()
	WHERE account_rental_policies.group_id=EXCLUDED.group_id OR NOT EXISTS(SELECT 1 FROM accounts WHERE rental_policy_id=account_rental_policies.id) RETURNING id`, p.Platform, p.GroupID, p.OwnerShareBPS, p.AdminUserID, p.Enabled).Scan(&policyID)
	if err == sql.ErrNoRows {
		return service.ErrRentalInvalid
	}
	if err != nil {
		return err
	}
	if err := enqueueRentalAccounts(ctx, tx, policyID, 0); err != nil {
		return err
	}
	return tx.Commit()
}

func enqueueRentalAccounts(ctx context.Context, tx *sql.Tx, policyID, accountID int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO scheduler_outbox(event_type,account_id) SELECT $1,a.id FROM accounts a LEFT JOIN accounts parent ON parent.id=a.parent_account_id WHERE ($2::bigint>0 AND COALESCE(a.rental_policy_id,parent.rental_policy_id)=$2) OR ($3::bigint>0 AND (a.id=$3 OR a.parent_account_id=$3))`, service.SchedulerOutboxEventAccountChanged, policyID, accountID)
	return err
}

func (r *tokenBankRepository) SetStatus(ctx context.Context, ownerID, accountID int64, status string) error {
	if status != "active" && status != "paused" {
		return service.ErrRentalInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE accounts SET rental_status=$3,schedulable=($3::varchar='active'),updated_at=NOW() WHERE id=$1 AND owner_user_id=$2 AND deleted_at IS NULL`, accountID, ownerID, status)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return service.ErrRentalNotFound
	}
	if err := enqueueRentalAccounts(ctx, tx, 0, accountID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *tokenBankRepository) Overview(ctx context.Context, ownerID int64, platform, status string, limit, offset int) (*service.RentalOverview, error) {
	out := &service.RentalOverview{Accounts: []service.RentalAccount{}}
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE owner_user_id IS NOT NULL AND deleted_at IS NULL AND ($1::bigint=0 OR owner_user_id=$1) AND ($2='' OR platform=$2) AND ($3='' OR rental_status=$3)`, ownerID, platform, status).Scan(&out.TotalAccounts)
	if err != nil {
		return nil, err
	}
	err = r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(owner_amount),0),COALESCE(SUM(owner_amount) FILTER(WHERE created_at>=date_trunc('day',NOW() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'),0),COALESCE(SUM(admin_amount),0) FROM account_revenue_ledger WHERE ($1::bigint=0 OR owner_user_id=$1) AND ($2='' OR platform=$2)`, ownerID, platform).Scan(&out.TotalRevenue, &out.TodayRevenue, &out.AdminRevenue)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT a.id,a.owner_user_id,a.name,a.platform,a.type,a.status,a.rental_status,(a.schedulable AND p.enabled AND a.rental_status='active'),COALESCE(u.requests,0),COALESCE(u.tokens,0),COALESCE(l.revenue,0)
	FROM accounts a JOIN account_rental_policies p ON p.id=a.rental_policy_id
	LEFT JOIN LATERAL(SELECT COUNT(*) requests,COALESCE(SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),0) tokens FROM usage_logs WHERE account_id=a.id OR account_id IN (SELECT id FROM accounts WHERE parent_account_id=a.id)) u ON true
	LEFT JOIN LATERAL(SELECT SUM(owner_amount) revenue FROM account_revenue_ledger WHERE owner_account_id=a.id) l ON true
	WHERE a.deleted_at IS NULL AND ($1::bigint=0 OR a.owner_user_id=$1) AND ($2='' OR a.platform=$2) AND ($5='' OR a.rental_status=$5) ORDER BY a.id DESC LIMIT $3 OFFSET $4`, ownerID, platform, limit, offset, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a service.RentalAccount
		if err := rows.Scan(&a.ID, &a.OwnerUserID, &a.Name, &a.Platform, &a.Type, &a.Status, &a.RentalStatus, &a.Schedulable, &a.Requests, &a.Tokens, &a.Revenue); err != nil {
			return nil, err
		}
		out.Accounts = append(out.Accounts, a)
	}
	return out, rows.Err()
}

func (r *tokenBankRepository) Revenue(ctx context.Context, ownerID, accountID int64, platform string, limit, offset int) (*service.RentalRevenuePage, error) {
	out := &service.RentalRevenuePage{Items: []service.RentalRevenue{}}
	if accountID > 0 && ownerID > 0 {
		var id int64
		if err := r.db.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND owner_user_id=$2`, accountID, ownerID).Scan(&id); err != nil {
			return nil, service.ErrRentalNotFound
		}
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_revenue_ledger WHERE ($1::bigint=0 OR owner_user_id=$1) AND ($2::bigint=0 OR owner_account_id=$2) AND ($3='' OR platform=$3)`, ownerID, accountID, platform).Scan(&out.Total); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,owner_account_id,owner_user_id,platform,model,billing_type,input_tokens,output_tokens,cache_tokens,bill_amount,owner_share_bps,owner_amount,admin_amount,created_at FROM account_revenue_ledger WHERE ($1::bigint=0 OR owner_user_id=$1) AND ($2::bigint=0 OR owner_account_id=$2) AND ($5='' OR platform=$5) ORDER BY id DESC LIMIT $3 OFFSET $4`, ownerID, accountID, limit, offset, platform)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l service.RentalRevenue
		if err := rows.Scan(&l.ID, &l.AccountID, &l.OwnerUserID, &l.Platform, &l.Model, &l.BillingType, &l.InputTokens, &l.OutputTokens, &l.CacheTokens, &l.BillAmount, &l.OwnerShareBPS, &l.OwnerAmount, &l.AdminAmount, &l.CreatedAt); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, l)
	}
	return out, rows.Err()
}

func (r *accountRepository) loadRentalSnapshots(ctx context.Context, accounts []*dbent.Account) (map[int64]*service.RentalSnapshot, error) {
	ids := []int64{}
	for _, a := range accounts {
		if a.OwnerUserID != nil || a.ParentAccountID != nil {
			ids = append(ids, a.ID)
		}
	}
	out := map[int64]*service.RentalSnapshot{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT a.id,o.id,o.owner_user_id,p.admin_user_id,p.id,p.version,p.owner_share_bps,p.platform,p.group_id,p.enabled,o.rental_status
	FROM accounts a JOIN accounts o ON o.id=COALESCE(a.parent_account_id,a.id) JOIN account_rental_policies p ON p.id=o.rental_policy_id WHERE a.id=ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		s := &service.RentalSnapshot{}
		if err := rows.Scan(&id, &s.OwnerAccountID, &s.OwnerUserID, &s.AdminUserID, &s.PolicyID, &s.PolicyVersion, &s.OwnerShareBPS, &s.Platform, &s.GroupID, &s.Enabled, &s.Status); err != nil {
			return nil, err
		}
		out[id] = s
	}
	return out, rows.Err()
}

func attachRentalSnapshot(a *service.Account, snapshot *service.RentalSnapshot) {
	if a == nil || snapshot == nil {
		return
	}
	a.Rental = snapshot
	a.OwnerUserID = &snapshot.OwnerUserID
	a.RentalStatus = snapshot.Status
}
