package tokencenter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrBusinessNotFound = errors.New("记录不存在")
var ErrBusinessConflict = errors.New("记录已更新或业务状态冲突，请刷新后重试")
var ErrBusinessInvalid = errors.New("请检查填写内容")
var ErrBusinessForbidden = errors.New("没有此记录的操作权限")

type BusinessAudit struct {
	ID        int64              `json:"id,string"`
	Revision  int64              `json:"revision"`
	History   []ApplicationEvent `json:"history"`
	CreatedAt time.Time          `json:"createdAt"`
	UpdatedAt time.Time          `json:"updatedAt"`
}
type BusinessReview struct {
	Revision int64  `json:"revision"`
	Status   string `json:"status"`
	Note     string `json:"note"`
}
type MerchantProfile struct {
	Name    string `json:"name"`
	SiteURL string `json:"siteUrl"`
	Contact string `json:"contact"`
	Message string `json:"message"`
}
type BusinessMerchant struct {
	BusinessAudit
	UserID       int64           `json:"userId,string"`
	AccountEmail string          `json:"accountEmail"`
	Profile      MerchantProfile `json:"profile"`
	Status       string          `json:"status"`
	Note         string          `json:"note"`
}
type ClaimEvidence struct {
	Contact   string `json:"contact"`
	ProofURL  string `json:"proofUrl"`
	Statement string `json:"statement"`
}
type PlatformClaim struct {
	BusinessAudit
	MerchantID int64         `json:"merchantId,string"`
	PlatformID string        `json:"platformId"`
	Platform   PlatformItem  `json:"platform"`
	Evidence   ClaimEvidence `json:"evidence"`
	Status     string        `json:"status"`
	Note       string        `json:"note"`
}
type AdSlot struct {
	BusinessAudit
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Mounted     bool   `json:"mounted"`
}
type AdCreative struct {
	Brand          string `json:"brand"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	ImageURL       string `json:"imageUrl"`
	DestinationURL string `json:"destinationUrl"`
	ButtonText     string `json:"buttonText"`
}
type AdCampaignInput struct {
	Revision int64      `json:"revision"`
	ClaimID  int64      `json:"claimId,string"`
	SlotID   int64      `json:"slotId,string"`
	Creative AdCreative `json:"creative"`
	StartsAt time.Time  `json:"startsAt"`
	EndsAt   time.Time  `json:"endsAt"`
	Submit   bool       `json:"submit"`
}
type AdCampaign struct {
	BusinessAudit
	MerchantID     int64      `json:"merchantId,string"`
	ClaimID        int64      `json:"claimId,string"`
	SlotID         int64      `json:"slotId,string"`
	Creative       AdCreative `json:"creative"`
	StartsAt       time.Time  `json:"startsAt"`
	EndsAt         time.Time  `json:"endsAt"`
	Status         string     `json:"status"`
	Note           string     `json:"note"`
	DeliveryStatus string     `json:"deliveryStatus"`
}
type BusinessPage[T any] struct {
	Items      []T    `json:"items"`
	NextBefore string `json:"nextBefore,omitempty"`
}

func invalidBusiness(message string) error { return fmt.Errorf("%w：%s", ErrBusinessInvalid, message) }
func normalizeMerchant(p MerchantProfile) (MerchantProfile, error) {
	p.Name = strings.TrimSpace(p.Name)
	p.SiteURL = strings.TrimSpace(p.SiteURL)
	p.Contact = strings.TrimSpace(p.Contact)
	p.Message = strings.TrimSpace(p.Message)
	if p.Name == "" || p.Contact == "" || !webURL(p.SiteURL) {
		return p, invalidBusiness("请填写商家名称、官网地址和联系方式")
	}
	return p, nil
}
func normalizeEvidence(p ClaimEvidence) (ClaimEvidence, error) {
	p.Contact = strings.TrimSpace(p.Contact)
	p.ProofURL = strings.TrimSpace(p.ProofURL)
	p.Statement = strings.TrimSpace(p.Statement)
	if p.Contact == "" || p.Statement == "" || !webURL(p.ProofURL) {
		return p, invalidBusiness("请填写认领说明、联系方式及可核验的证明链接")
	}
	return p, nil
}
func normalizeCampaign(in AdCampaignInput) (AdCampaignInput, error) {
	c := &in.Creative
	c.Brand = strings.TrimSpace(c.Brand)
	c.Title = strings.TrimSpace(c.Title)
	c.Description = strings.TrimSpace(c.Description)
	c.ImageURL = strings.TrimSpace(c.ImageURL)
	c.DestinationURL = strings.TrimSpace(c.DestinationURL)
	c.ButtonText = strings.TrimSpace(c.ButtonText)
	if in.ClaimID <= 0 || in.SlotID <= 0 || c.Brand == "" || c.Title == "" || c.ButtonText == "" || !webURL(c.DestinationURL) || (c.ImageURL != "" && !webURL(c.ImageURL)) {
		return in, invalidBusiness("请选择认领平台和广告位，并填写品牌、标题、按钮文字与有效链接")
	}
	if in.StartsAt.IsZero() || in.EndsAt.IsZero() || !in.EndsAt.After(in.StartsAt) {
		return in, invalidBusiness("结束时间必须晚于开始时间")
	}
	return in, nil
}
func validReview(in BusinessReview, statuses ...string) error {
	if in.Revision <= 0 || strings.TrimSpace(in.Note) == "" {
		return invalidBusiness("请填写处理说明并使用当前版本")
	}
	for _, s := range statuses {
		if in.Status == s {
			return nil
		}
	}
	return invalidBusiness("未知处理状态")
}
func (s *Service) claimPlatform(ctx context.Context, id string) (PlatformItem, error) {
	c, err := s.BrowseCatalog(ctx)
	if err != nil {
		return PlatformItem{}, err
	}
	if c.Demo {
		return PlatformItem{}, invalidBusiness("演示平台不能认领")
	}
	for _, group := range projectPlatformDirectory(c).Groups {
		for _, item := range group.Items {
			if item.ID == id {
				return item, nil
			}
		}
	}
	return PlatformItem{}, invalidBusiness("请选择中台已发布的平台；尚未收录的平台请先申请收录")
}
func campaignDelivery(c AdCampaign, now time.Time) string {
	if c.Status != "approved" {
		return c.Status
	}
	if !c.EndsAt.After(now) {
		return "expired"
	}
	if c.StartsAt.After(now) {
		return "scheduled"
	}
	return "live"
}
