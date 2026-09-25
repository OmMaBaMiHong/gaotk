package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type tokenBankRepository struct{ db *sql.DB }

func NewTokenBankRepository(db *sql.DB) service.TokenBankRepository {
	return &tokenBankRepository{db: db}
}

func (r *tokenBankRepository) Overview(ctx context.Context, ownerID int64, platform, status string, limit, offset int) (*service.RentalOverview, error) {
	out := &service.RentalOverview{Accounts: []service.RentalAccount{}}
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE owner_user_id IS NOT NULL AND parent_account_id IS NULL AND deleted_at IS NULL AND ($1::bigint=0 OR owner_user_id=$1) AND ($2='' OR platform=$2) AND ($3='' OR status=$3)`, ownerID, platform, status).Scan(&out.TotalAccounts)
	if err != nil {
		return nil, err
	}
	err = r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(owner_amount),0),COALESCE(SUM(owner_amount) FILTER(WHERE created_at>=date_trunc('day',NOW() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'),0),COALESCE(SUM(admin_amount),0) FROM account_revenue_ledger WHERE ($1::bigint=0 OR owner_user_id=$1) AND ($2='' OR platform=$2)`, ownerID, platform).Scan(&out.TotalRevenue, &out.TodayRevenue, &out.AdminRevenue)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT a.id,a.owner_user_id,a.name,a.platform,a.type,a.status,(a.schedulable AND a.status='active'),COALESCE(u.requests,0),COALESCE(u.tokens,0),COALESCE(l.revenue,0)
	FROM accounts a
	LEFT JOIN LATERAL(SELECT COUNT(*) requests,COALESCE(SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),0) tokens FROM usage_logs WHERE account_id=a.id OR account_id IN (SELECT id FROM accounts WHERE parent_account_id=a.id)) u ON true
	LEFT JOIN LATERAL(SELECT SUM(owner_amount) revenue FROM account_revenue_ledger WHERE owner_account_id=a.id) l ON true
	WHERE a.owner_user_id IS NOT NULL AND a.parent_account_id IS NULL AND a.deleted_at IS NULL AND ($1::bigint=0 OR a.owner_user_id=$1) AND ($2='' OR a.platform=$2) AND ($5='' OR a.status=$5) ORDER BY a.id DESC LIMIT $3 OFFSET $4`, ownerID, platform, limit, offset, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a service.RentalAccount
		if err := rows.Scan(&a.ID, &a.OwnerUserID, &a.Name, &a.Platform, &a.Type, &a.Status, &a.Schedulable, &a.Requests, &a.Tokens, &a.Revenue); err != nil {
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
	rows, err := r.sql.QueryContext(ctx, `SELECT a.id,o.id,o.owner_user_id,o.platform,o.type,o.status,(o.schedulable AND o.deleted_at IS NULL),
	COALESCE(ag.group_id,0),COALESCE(g.platform,''),COALESCE(g.status,''),COALESCE(c.id,0),COALESCE(c.status,''),COALESCE(c.features_config,'{}'::jsonb),
	COALESCE(o.credentials->>'savings_verified_plan_type',''),COALESCE(o.credentials->>'plan_type','')
	FROM accounts a JOIN accounts o ON o.id=COALESCE(a.parent_account_id,a.id)
	LEFT JOIN account_groups ag ON ag.account_id=a.id
	LEFT JOIN groups g ON g.id=ag.group_id AND g.deleted_at IS NULL
	LEFT JOIN channel_groups cg ON cg.group_id=ag.group_id
	LEFT JOIN channels c ON c.id=cg.channel_id
	WHERE a.id=ANY($1) AND o.owner_user_id IS NOT NULL`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, groupID, channelID int64
		var channelStatus, groupPlatform, groupStatus string
		var configJSON []byte
		s := &service.RentalSnapshot{}
		if err := rows.Scan(&id, &s.OwnerAccountID, &s.OwnerUserID, &s.Platform, &s.OwnerAccountType, &s.Status, &s.Schedulable, &groupID, &groupPlatform, &groupStatus, &channelID, &channelStatus, &configJSON, &s.OwnerVerifiedPlan, &s.OwnerCurrentPlan); err != nil {
			return nil, err
		}
		base := out[id]
		if base == nil {
			base = s
			base.Channels = map[int64]*service.RentalSnapshot{}
			out[id] = base
		}
		if channelID == 0 {
			continue
		}
		channel := &service.Channel{ID: channelID, Status: channelStatus}
		if err := json.Unmarshal(configJSON, &channel.FeaturesConfig); err != nil {
			return nil, fmt.Errorf("decode token savings channel %d: %w", channelID, err)
		}
		config, err := channel.SavingsConfig()
		if err != nil {
			return nil, fmt.Errorf("load token savings channel %d: %w", channelID, err)
		}
		if config == nil {
			continue
		}
		selected := *s
		selected.Channels = nil
		selected.ChannelID, selected.GroupID = channelID, groupID
		receiving := false
		for _, id := range config.ReceivingGroupIDs {
			if id == groupID {
				receiving = true
			}
		}
		selected.AdminUserID, selected.OwnerShareBPS, selected.Enabled = config.AdminUserID, config.OwnerShareBPS, config.Enabled && channel.IsActive() && receiving && groupStatus == service.StatusActive && groupPlatform == s.Platform
		selected.AllowedAccountTypes = config.ReceivingRule(groupID).TypesForPlatform(s.Platform)
		if rule := config.ReceivingRule(groupID); rule != nil {
			selected.AllowedPlans = rule.AllowedPlans
		}
		base.Channels[groupID] = &selected
	}
	return out, rows.Err()
}

func attachRentalSnapshot(a *service.Account, snapshot *service.RentalSnapshot) {
	if a == nil || snapshot == nil {
		return
	}
	a.Rental = snapshot
	a.OwnerUserID = &snapshot.OwnerUserID
}
