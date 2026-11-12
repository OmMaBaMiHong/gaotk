package tokencenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

const businessAuditJSON = `'id',id::text,'revision',revision,'history',history,'createdAt',created_at,'updatedAt',updated_at`
const merchantJSON = `jsonb_build_object(` + businessAuditJSON + `,'userId',account_user_id::text,'accountEmail',account_email,'profile',profile,'status',status,'note',review_note)`
const claimJSON = `jsonb_build_object(` + businessAuditJSON + `,'merchantId',merchant_id::text,'platformId',platform_id,'platform',platform,'evidence',evidence,'status',status,'note',review_note)`
const slotJSON = `jsonb_build_object(` + businessAuditJSON + `,'code',code,'name',name,'description',description,'enabled',enabled,'mounted',code='home-sponsor')`
const campaignJSON = `jsonb_build_object(` + businessAuditJSON + `,'merchantId',merchant_id::text,'claimId',claim_id::text,'slotId',slot_id::text,'creative',creative,'startsAt',starts_at,'endsAt',ends_at,'status',status,'note',review_note)`

type businessQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func businessDBError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBusinessNotFound
	}
	var pg *pq.Error
	if errors.As(err, &pg) && pg.Code == "23505" {
		return fmt.Errorf("%w：该平台已有有效认领、待审申请或相同编号的广告位", ErrBusinessConflict)
	}
	return err
}
func scanBusiness[T any](row applicationScanner) (T, error) {
	var value T
	var data []byte
	if err := row.Scan(&data); err != nil {
		return value, businessDBError(err)
	}
	err := json.Unmarshal(data, &value)
	return value, err
}
func jsonValue(value any) string { b, _ := json.Marshal(value); return string(b) }
func businessHistory(status, note, actor string) string {
	return jsonValue([]ApplicationEvent{{At: time.Now().UTC(), Status: status, Note: note, Actor: actor}})
}
func businessTransaction[T any](ctx context.Context, db *sql.DB, work func(*sql.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	v, err := work(tx)
	if err != nil {
		return zero, businessDBError(err)
	}
	if err = tx.Commit(); err != nil {
		return zero, businessDBError(err)
	}
	return v, nil
}
func (a BusinessAudit) businessID() int64 { return a.ID }
func businessList[T interface{ businessID() int64 }](ctx context.Context, q businessQuery, table, projection, where string, args ...any) (BusinessPage[T], error) {
	page := BusinessPage[T]{Items: []T{}}
	rows, err := q.QueryContext(ctx, "SELECT "+projection+" FROM "+table+" WHERE "+where+" ORDER BY id DESC LIMIT 51", args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanBusiness[T](rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, v)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > 50 {
		page.Items = page.Items[:50]
		page.NextBefore = strconv.FormatInt(page.Items[49].businessID(), 10)
	}
	return page, nil
}
func merchantByAccount(ctx context.Context, q businessQuery, u BusinessIdentity, lock bool) (BusinessMerchant, error) {
	query := "SELECT " + merchantJSON + " FROM tokencenter_merchants WHERE account_issuer=$1 AND account_user_id=$2"
	if lock {
		query += " FOR UPDATE"
	}
	return scanBusiness[BusinessMerchant](q.QueryRowContext(ctx, query, u.Issuer, u.ID))
}
func lockMerchant(ctx context.Context, q businessQuery, id int64) (BusinessMerchant, error) {
	return scanBusiness[BusinessMerchant](q.QueryRowContext(ctx, "SELECT "+merchantJSON+" FROM tokencenter_merchants WHERE id=$1 FOR UPDATE", id))
}
func (s *SQLStore) MerchantForAccount(ctx context.Context, u BusinessIdentity) (BusinessMerchant, error) {
	return merchantByAccount(ctx, s.db, u, false)
}
func (s *SQLStore) Merchants(ctx context.Context, status string, before int64) (BusinessPage[BusinessMerchant], error) {
	return businessList[BusinessMerchant](ctx, s.db, "tokencenter_merchants", merchantJSON, "($1='' OR status=$1) AND ($2::bigint=0 OR id<$2)", status, before)
}
func pauseMerchantAds(ctx context.Context, q businessQuery, merchantID, claimID int64, note, actor string) error {
	_, err := q.ExecContext(ctx, `UPDATE tokencenter_ad_campaigns SET status='paused',review_note=$3,history=history||$4::jsonb,revision=revision+1,updated_at=NOW() WHERE merchant_id=$1 AND ($2::bigint=0 OR claim_id=$2) AND status='approved'`, merchantID, claimID, note, businessHistory("paused", note, actor))
	return err
}
func (s *SQLStore) SaveMerchant(ctx context.Context, u BusinessIdentity, p MerchantProfile, revision int64) (BusinessMerchant, error) {
	p, err := normalizeMerchant(p)
	if err != nil {
		return BusinessMerchant{}, err
	}
	actor := u.Issuer + ":user:" + strconv.FormatInt(u.ID, 10)
	return businessTransaction(ctx, s.db, func(tx *sql.Tx) (BusinessMerchant, error) {
		if revision == 0 {
			return scanBusiness[BusinessMerchant](tx.QueryRowContext(ctx, `INSERT INTO tokencenter_merchants(account_issuer,account_user_id,account_email,profile,history) VALUES($1,$2,$3,$4::jsonb,$5::jsonb) RETURNING `+merchantJSON, u.Issuer, u.ID, u.Email, jsonValue(p), businessHistory("pending", "提交商家入驻", actor)))
		}
		row, err := merchantByAccount(ctx, tx, u, true)
		if err != nil {
			return row, err
		}
		if row.Revision != revision {
			return row, ErrBusinessConflict
		}
		if row.Status == "suspended" {
			return row, invalidBusiness("商家已停用，请联系运营处理")
		}
		if err = pauseMerchantAds(ctx, tx, row.ID, 0, "商家资料变更，投放暂停等待复审", actor); err != nil {
			return row, err
		}
		return scanBusiness[BusinessMerchant](tx.QueryRowContext(ctx, `UPDATE tokencenter_merchants SET profile=$2::jsonb,account_email=$3,status='pending',review_note='',revision=revision+1,history=history||$4::jsonb,updated_at=NOW() WHERE id=$1 RETURNING `+merchantJSON, row.ID, jsonValue(p), u.Email, businessHistory("pending", "修改资料并重新提交审核", actor)))
	})
}
func (s *SQLStore) ReviewMerchant(ctx context.Context, id int64, in BusinessReview, actor string) (BusinessMerchant, error) {
	if err := validReview(in, "approved", "declined", "suspended"); err != nil {
		return BusinessMerchant{}, err
	}
	return businessTransaction(ctx, s.db, func(tx *sql.Tx) (BusinessMerchant, error) {
		row, err := lockMerchant(ctx, tx, id)
		if err != nil {
			return row, err
		}
		if row.Revision != in.Revision {
			return row, ErrBusinessConflict
		}
		if in.Status != "approved" {
			if err = pauseMerchantAds(ctx, tx, id, 0, "商家状态变更："+in.Note, actor); err != nil {
				return row, err
			}
		}
		return scanBusiness[BusinessMerchant](tx.QueryRowContext(ctx, `UPDATE tokencenter_merchants SET status=$2,review_note=$3,revision=revision+1,history=history||$4::jsonb,updated_at=NOW() WHERE id=$1 RETURNING `+merchantJSON, id, in.Status, in.Note, businessHistory(in.Status, in.Note, actor)))
	})
}
func (s *SQLStore) Claims(ctx context.Context, merchantID int64, status string, before int64) (BusinessPage[PlatformClaim], error) {
	return businessList[PlatformClaim](ctx, s.db, "tokencenter_platform_claims", claimJSON, "($1::bigint=0 OR merchant_id=$1) AND ($2='' OR status=$2) AND ($3::bigint=0 OR id<$3)", merchantID, status, before)
}
func (s *SQLStore) Claim(ctx context.Context, id int64) (PlatformClaim, error) {
	return scanBusiness[PlatformClaim](s.db.QueryRowContext(ctx, "SELECT "+claimJSON+" FROM tokencenter_platform_claims WHERE id=$1", id))
}
func (s *SQLStore) SubmitClaim(ctx context.Context, merchantID int64, platform PlatformItem, evidence ClaimEvidence, actor string) (PlatformClaim, error) {
	evidence, err := normalizeEvidence(evidence)
	if err != nil {
		return PlatformClaim{}, err
	}
	return businessTransaction(ctx, s.db, func(tx *sql.Tx) (PlatformClaim, error) {
		merchant, err := lockMerchant(ctx, tx, merchantID)
		if err != nil {
			return PlatformClaim{}, err
		}
		if merchant.Status != "approved" {
			return PlatformClaim{}, invalidBusiness("商家入驻审核通过后可申请认领")
		}
		var owned bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tokencenter_platform_claims WHERE platform_id=$1 AND status='approved')`, platform.ID).Scan(&owned); err != nil {
			return PlatformClaim{}, err
		}
		if owned {
			return PlatformClaim{}, fmt.Errorf("%w：平台已有认领归属，请联系运营核查", ErrBusinessConflict)
		}
		return scanBusiness[PlatformClaim](tx.QueryRowContext(ctx, `INSERT INTO tokencenter_platform_claims(merchant_id,platform_id,platform,evidence,history) VALUES($1,$2,$3::jsonb,$4::jsonb,$5::jsonb) RETURNING `+claimJSON, merchantID, platform.ID, jsonValue(platform), jsonValue(evidence), businessHistory("pending", "提交平台认领", actor)))
	})
}
func (s *SQLStore) ReviewClaim(ctx context.Context, id int64, in BusinessReview, actor string) (PlatformClaim, error) {
	if err := validReview(in, "approved", "declined", "revoked"); err != nil {
		return PlatformClaim{}, err
	}
	initial, err := s.Claim(ctx, id)
	if err != nil {
		return initial, err
	}
	return businessTransaction(ctx, s.db, func(tx *sql.Tx) (PlatformClaim, error) {
		merchant, err := lockMerchant(ctx, tx, initial.MerchantID)
		if err != nil {
			return PlatformClaim{}, err
		}
		row, err := scanBusiness[PlatformClaim](tx.QueryRowContext(ctx, "SELECT "+claimJSON+" FROM tokencenter_platform_claims WHERE id=$1 FOR UPDATE", id))
		if err != nil {
			return row, err
		}
		if row.Revision != in.Revision {
			return row, ErrBusinessConflict
		}
		if (row.Status != "pending" && !(row.Status == "approved" && in.Status == "revoked")) || (row.Status == "pending" && in.Status == "revoked") {
			return row, invalidBusiness("待审核认领可通过或退回，已认领平台可撤销")
		}
		if in.Status == "approved" && merchant.Status != "approved" {
			return row, invalidBusiness("商家入驻尚未通过审核")
		}
		if in.Status == "revoked" {
			if err = pauseMerchantAds(ctx, tx, row.MerchantID, row.ID, "平台认领已撤销："+in.Note, actor); err != nil {
				return row, err
			}
		}
		return scanBusiness[PlatformClaim](tx.QueryRowContext(ctx, `UPDATE tokencenter_platform_claims SET status=$2,review_note=$3,revision=revision+1,history=history||$4::jsonb,updated_at=NOW() WHERE id=$1 RETURNING `+claimJSON, id, in.Status, in.Note, businessHistory(in.Status, in.Note, actor)))
	})
}
func (s *SQLStore) Slots(ctx context.Context, before int64) (BusinessPage[AdSlot], error) {
	return businessList[AdSlot](ctx, s.db, "tokencenter_ad_slots", slotJSON, "($1::bigint=0 OR id<$1)", before)
}
func (s *SQLStore) SaveSlot(ctx context.Context, id int64, in AdSlot, actor string) (AdSlot, error) {
	in.Code = strings.TrimSpace(in.Code)
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" || in.Code == "" {
		return AdSlot{}, invalidBusiness("请填写广告位编号和名称")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(in.Code) {
		return AdSlot{}, invalidBusiness("广告位编号只能使用字母、数字、短横线或下划线")
	}
	note := "更新广告位"
	if !in.Enabled {
		note = "停用广告位"
	}
	if id == 0 {
		return scanBusiness[AdSlot](s.db.QueryRowContext(ctx, `INSERT INTO tokencenter_ad_slots(code,name,description,enabled,history) VALUES($1,$2,$3,$4,$5::jsonb) RETURNING `+slotJSON, in.Code, in.Name, in.Description, in.Enabled, businessHistory("created", "创建广告位", actor)))
	}
	row, err := scanBusiness[AdSlot](s.db.QueryRowContext(ctx, `UPDATE tokencenter_ad_slots SET name=$3,description=$4,enabled=$5,revision=revision+1,history=history||$6::jsonb,updated_at=NOW() WHERE id=$1 AND revision=$2 AND code=$7 RETURNING `+slotJSON, id, in.Revision, in.Name, in.Description, in.Enabled, businessHistory("updated", note, actor), in.Code))
	if errors.Is(err, ErrBusinessNotFound) {
		err = ErrBusinessConflict
	}
	return row, err
}
func (s *SQLStore) Campaign(ctx context.Context, id int64) (AdCampaign, error) {
	return scanBusiness[AdCampaign](s.db.QueryRowContext(ctx, "SELECT "+campaignJSON+" FROM tokencenter_ad_campaigns WHERE id=$1", id))
}
func (s *SQLStore) Campaigns(ctx context.Context, merchantID int64, status string, before int64) (BusinessPage[AdCampaign], error) {
	page, err := businessList[AdCampaign](ctx, s.db, "tokencenter_ad_campaigns", campaignJSON, "($1::bigint=0 OR merchant_id=$1) AND ($2='' OR status=$2) AND ($3::bigint=0 OR id<$3)", merchantID, status, before)
	if err != nil {
		return page, err
	}
	// One single-table slot lookup for this page, never a per-row query.
	ids := []int64{}
	for _, a := range page.Items {
		ids = append(ids, a.SlotID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,enabled FROM tokencenter_ad_slots WHERE id=ANY($1)`, pq.Array(ids))
	if err != nil {
		return page, err
	}
	defer rows.Close()
	enabled := map[int64]bool{}
	for rows.Next() {
		var id int64
		var value bool
		if err = rows.Scan(&id, &value); err != nil {
			return page, err
		}
		enabled[id] = value
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	for i := range page.Items {
		a := &page.Items[i]
		a.DeliveryStatus = campaignDelivery(*a, time.Now())
		if a.Status == "approved" && !enabled[a.SlotID] {
			a.DeliveryStatus = "slot_disabled"
		}
	}
	return page, nil
}
func campaignOwner(ctx context.Context, tx *sql.Tx, merchantID, claimID, slotID int64) (AdSlot, error) {
	m, err := lockMerchant(ctx, tx, merchantID)
	if err != nil {
		return AdSlot{}, err
	}
	if m.Status != "approved" {
		return AdSlot{}, invalidBusiness("商家入驻尚未通过审核")
	}
	claim, err := scanBusiness[PlatformClaim](tx.QueryRowContext(ctx, "SELECT "+claimJSON+" FROM tokencenter_platform_claims WHERE id=$1 FOR UPDATE", claimID))
	if err != nil {
		return AdSlot{}, err
	}
	if claim.MerchantID != merchantID {
		return AdSlot{}, ErrBusinessForbidden
	}
	if claim.Status != "approved" {
		return AdSlot{}, invalidBusiness("只能为已认领的平台投放广告")
	}
	slot, err := scanBusiness[AdSlot](tx.QueryRowContext(ctx, "SELECT "+slotJSON+" FROM tokencenter_ad_slots WHERE id=$1 FOR UPDATE", slotID))
	if err != nil {
		return slot, err
	}
	if !slot.Enabled {
		return slot, invalidBusiness("广告位已停用")
	}
	return slot, nil
}
func (s *SQLStore) SaveCampaign(ctx context.Context, merchantID, id int64, in AdCampaignInput, actor string) (AdCampaign, error) {
	in, err := normalizeCampaign(in)
	if err != nil {
		return AdCampaign{}, err
	}
	return businessTransaction(ctx, s.db, func(tx *sql.Tx) (AdCampaign, error) {
		if _, err := campaignOwner(ctx, tx, merchantID, in.ClaimID, in.SlotID); err != nil {
			return AdCampaign{}, err
		}
		status := "draft"
		if in.Submit {
			status = "pending"
		}
		history := businessHistory(status, "保存广告素材与排期", actor)
		if id == 0 {
			return scanBusiness[AdCampaign](tx.QueryRowContext(ctx, `INSERT INTO tokencenter_ad_campaigns(merchant_id,claim_id,slot_id,creative,starts_at,ends_at,status,history) VALUES($1,$2,$3,$4::jsonb,$5,$6,$7,$8::jsonb) RETURNING `+campaignJSON, merchantID, in.ClaimID, in.SlotID, jsonValue(in.Creative), in.StartsAt, in.EndsAt, status, history))
		}
		row, err := scanBusiness[AdCampaign](tx.QueryRowContext(ctx, `UPDATE tokencenter_ad_campaigns SET claim_id=$4,slot_id=$5,creative=$6::jsonb,starts_at=$7,ends_at=$8,status=$9,review_note='',revision=revision+1,history=history||$10::jsonb,updated_at=NOW() WHERE id=$1 AND merchant_id=$2 AND revision=$3 RETURNING `+campaignJSON, id, merchantID, in.Revision, in.ClaimID, in.SlotID, jsonValue(in.Creative), in.StartsAt, in.EndsAt, status, history))
		if errors.Is(err, ErrBusinessNotFound) {
			err = ErrBusinessConflict
		}
		return row, err
	})
}
func (s *SQLStore) ReviewCampaign(ctx context.Context, id int64, in BusinessReview, actor string) (AdCampaign, error) {
	if err := validReview(in, "approved", "declined", "paused"); err != nil {
		return AdCampaign{}, err
	}
	initial, err := s.Campaign(ctx, id)
	if err != nil {
		return initial, err
	}
	return businessTransaction(ctx, s.db, func(tx *sql.Tx) (AdCampaign, error) {
		// All ownership mutations lock merchant -> claim -> slot -> campaign.
		if in.Status == "approved" {
			if _, err = campaignOwner(ctx, tx, initial.MerchantID, initial.ClaimID, initial.SlotID); err != nil {
				return AdCampaign{}, err
			}
		} else {
			if _, err = lockMerchant(ctx, tx, initial.MerchantID); err != nil {
				return AdCampaign{}, err
			}
		}
		row, err := scanBusiness[AdCampaign](tx.QueryRowContext(ctx, "SELECT "+campaignJSON+" FROM tokencenter_ad_campaigns WHERE id=$1 FOR UPDATE", id))
		if err != nil {
			return row, err
		}
		if row.Revision != in.Revision {
			return row, ErrBusinessConflict
		}
		if in.Status == "approved" {
			if row.Status != "pending" && row.Status != "paused" {
				return row, invalidBusiness("只可通过待审核或恢复已暂停的投放")
			}
			if !row.EndsAt.After(time.Now()) {
				return row, invalidBusiness("该档期已经结束，请商家修改排期后重新提交")
			}
			var overlap bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tokencenter_ad_campaigns WHERE slot_id=$1 AND status='approved' AND id<>$2 AND starts_at<$3 AND ends_at>$4)`, row.SlotID, id, row.EndsAt, row.StartsAt).Scan(&overlap)
			if err != nil {
				return row, err
			}
			if overlap {
				return row, fmt.Errorf("%w：该广告位的档期与已通过投放重叠", ErrBusinessConflict)
			}
		} else if in.Status == "declined" && row.Status != "pending" {
			return row, invalidBusiness("只能退回待审核的投放")
		}
		return scanBusiness[AdCampaign](tx.QueryRowContext(ctx, `UPDATE tokencenter_ad_campaigns SET status=$2,review_note=$3,revision=revision+1,history=history||$4::jsonb,updated_at=NOW() WHERE id=$1 RETURNING `+campaignJSON, id, in.Status, in.Note, businessHistory(in.Status, in.Note, actor)))
	})
}

type PublicCampaign struct {
	ID       int64      `json:"id,string"`
	Slot     string     `json:"slot"`
	Creative AdCreative `json:"creative"`
	StartsAt time.Time  `json:"startsAt"`
	EndsAt   time.Time  `json:"endsAt"`
}

func (s *SQLStore) PublicCampaign(ctx context.Context, code string) (*PublicCampaign, error) {
	var slotID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM tokencenter_ad_slots WHERE code=$1 AND enabled=TRUE`, code).Scan(&slotID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a, err := scanBusiness[AdCampaign](s.db.QueryRowContext(ctx, `SELECT `+campaignJSON+` FROM tokencenter_ad_campaigns WHERE slot_id=$1 AND status='approved' AND starts_at<=NOW() AND ends_at>NOW() ORDER BY starts_at,id LIMIT 1`, slotID))
	if errors.Is(err, ErrBusinessNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &PublicCampaign{ID: a.ID, Slot: code, Creative: a.Creative, StartsAt: a.StartsAt, EndsAt: a.EndsAt}, nil
}
