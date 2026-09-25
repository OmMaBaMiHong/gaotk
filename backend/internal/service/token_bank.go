package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrRentalNotFound = infraerrors.NotFound("RENTAL_NOT_FOUND", "储蓄账号不存在")

// RentalAccount intentionally contains no credentials or consumer identity.
type RentalAccount struct {
	ID          int64   `json:"id"`
	OwnerUserID int64   `json:"owner_user_id"`
	Name        string  `json:"name"`
	Platform    string  `json:"platform"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Schedulable bool    `json:"schedulable"`
	Requests    int64   `json:"requests"`
	Tokens      int64   `json:"tokens"`
	Revenue     float64 `json:"revenue"`
}

type RentalRevenue struct {
	ID            int64     `json:"id"`
	AccountID     int64     `json:"account_id"`
	OwnerUserID   int64     `json:"owner_user_id"`
	Platform      string    `json:"platform"`
	Model         string    `json:"model"`
	BillingType   int8      `json:"billing_type"`
	InputTokens   int64     `json:"input_tokens"`
	OutputTokens  int64     `json:"output_tokens"`
	CacheTokens   int64     `json:"cache_tokens"`
	BillAmount    float64   `json:"bill_amount"`
	OwnerShareBPS int       `json:"owner_share_bps"`
	OwnerAmount   float64   `json:"owner_amount"`
	AdminAmount   float64   `json:"admin_amount"`
	CreatedAt     time.Time `json:"created_at"`
}

type RentalOverview struct {
	Accounts      []RentalAccount `json:"accounts"`
	TotalAccounts int             `json:"total_accounts"`
	TotalRevenue  float64         `json:"total_revenue"`
	TodayRevenue  float64         `json:"today_revenue"`
	AdminRevenue  float64         `json:"admin_revenue"`
}

type RentalRevenuePage struct {
	Items []RentalRevenue `json:"items"`
	Total int             `json:"total"`
}

type TokenBankRepository interface {
	Overview(context.Context, int64, string, string, int, int) (*RentalOverview, error)
	Revenue(context.Context, int64, int64, string, int, int) (*RentalRevenuePage, error)
}
