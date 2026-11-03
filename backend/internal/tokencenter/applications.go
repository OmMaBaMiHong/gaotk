package tokencenter

// Applications are private contact/collection leads, never catalog facts or
// proof of site ownership. Public facts still arrive through Chaos admission.
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

var ErrApplicationNotFound = errors.New("申请不存在或查询凭证不正确")
var ErrApplicationConflict = errors.New("申请已被更新，请刷新后重试")
var ErrApplicationUnavailable = errors.New("入驻申请服务暂时不可用")
var ErrApplicationInvalid = errors.New("申请处理条件不满足")

type ApplicationInput struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	SiteURL    string `json:"siteUrl"`
	PricingURL string `json:"pricingUrl"`
	Contact    string `json:"contact"`
	Message    string `json:"message"`
}
type ApplicationEvent struct {
	At     time.Time `json:"at"`
	Status string    `json:"status"`
	Note   string    `json:"note"`
	Actor  string    `json:"actor"`
}
type ApplicationState struct {
	Status     string             `json:"status"`
	PublicNote string             `json:"publicNote"`
	SiteID     string             `json:"siteId,omitempty"`
	History    []ApplicationEvent `json:"history"`
}
type Application struct {
	ID int64 `json:"id,string"`
	ApplicationInput
	ApplicationState
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type ApplicationReceipt struct {
	Receipt    string    `json:"receipt,omitempty"`
	Status     string    `json:"status"`
	PublicNote string    `json:"publicNote"`
	SiteID     string    `json:"siteId,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
type ApplicationReview struct {
	Revision int64  `json:"revision"`
	Status   string `json:"status"`
	Note     string `json:"note"`
	SiteID   string `json:"siteId"`
}
type ApplicationPage struct {
	Items      []Application `json:"items"`
	NextBefore string        `json:"nextBefore,omitempty"`
}
type applicationStore interface {
	CreateApplication(context.Context, ApplicationInput, string) (Application, error)
	ApplicationByReceipt(context.Context, string) (Application, error)
	ApplicationByID(context.Context, int64) (Application, error)
	ListApplications(context.Context, string, int64) (ApplicationPage, error)
	ReviewApplication(context.Context, int64, ApplicationReview, string) (Application, error)
}

func normalizeApplication(in ApplicationInput) (ApplicationInput, error) {
	in.Kind, in.Name, in.SiteURL, in.PricingURL, in.Contact, in.Message = strings.TrimSpace(in.Kind), strings.TrimSpace(in.Name), strings.TrimSpace(in.SiteURL), strings.TrimSpace(in.PricingURL), strings.TrimSpace(in.Contact), strings.TrimSpace(in.Message)
	if in.Kind != "relay" && in.Kind != "partnership" {
		return in, fmt.Errorf("请选择中转站收录或平台合作")
	}
	if in.Name == "" || in.Contact == "" || !webURL(in.SiteURL) {
		return in, fmt.Errorf("请填写站点名称、有效官网地址和联系方式")
	}
	if in.PricingURL != "" && !webURL(in.PricingURL) {
		return in, fmt.Errorf("报价地址须为有效的 HTTP(S) 地址")
	}
	return in, nil
}
func receiptHash(receipt string) string {
	sum := sha256.Sum256([]byte(receipt))
	return hex.EncodeToString(sum[:])
}
func applicationReceipt(a Application) ApplicationReceipt {
	return ApplicationReceipt{Status: a.Status, PublicNote: a.PublicNote, SiteID: a.SiteID, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}
func (s *Service) applications() (applicationStore, error) {
	store, ok := s.store.(applicationStore)
	if !ok {
		return nil, ErrApplicationUnavailable
	}
	return store, nil
}
func (s *Service) SubmitApplication(ctx context.Context, input ApplicationInput) (ApplicationReceipt, error) {
	input, err := normalizeApplication(input)
	if err != nil {
		return ApplicationReceipt{}, err
	}
	store, err := s.applications()
	if err != nil {
		return ApplicationReceipt{}, err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return ApplicationReceipt{}, ErrApplicationUnavailable
	}
	receipt := hex.EncodeToString(secret)
	row, err := store.CreateApplication(ctx, input, receiptHash(receipt))
	if err != nil {
		return ApplicationReceipt{}, err
	}
	out := applicationReceipt(row)
	out.Receipt = receipt
	return out, nil
}
func (s *Service) ApplicationStatus(ctx context.Context, receipt string) (ApplicationReceipt, error) {
	decoded, err := hex.DecodeString(receipt)
	if err != nil || len(decoded) != 32 {
		return ApplicationReceipt{}, ErrApplicationNotFound
	}
	store, err := s.applications()
	if err != nil {
		return ApplicationReceipt{}, err
	}
	row, err := store.ApplicationByReceipt(ctx, receiptHash(receipt))
	if err != nil {
		return ApplicationReceipt{}, err
	}
	return applicationReceipt(row), nil
}
func applicationStatus(status string) bool {
	switch status {
	case "pending", "contacted", "accepted", "published", "declined":
		return true
	}
	return false
}
func sameSiteOrigin(a, b string) bool {
	x, e1 := url.Parse(a)
	y, e2 := url.Parse(b)
	return e1 == nil && e2 == nil && x.Host != "" && strings.EqualFold(x.Host, y.Host)
}
func (s *Service) ReviewApplication(ctx context.Context, id int64, review ApplicationReview, actor string) (Application, error) {
	if review.Revision <= 0 || !applicationStatus(review.Status) || strings.TrimSpace(review.Note) == "" {
		return Application{}, fmt.Errorf("%w：请选择处理状态并填写可向申请人反馈的处理说明", ErrApplicationInvalid)
	}
	store, err := s.applications()
	if err != nil {
		return Application{}, err
	}
	row, err := store.ApplicationByID(ctx, id)
	if err != nil {
		return Application{}, err
	}
	if review.Revision != row.Revision {
		return Application{}, ErrApplicationConflict
	}
	if review.Status == "published" {
		catalog, err := s.Catalog(ctx)
		if err != nil || catalog.Demo || catalog.RankingAuthority != "chaos" || !qualifiedContract(catalog.Admission) {
			return Application{}, fmt.Errorf("%w：目录尚不可用，不能标记为已收录", ErrApplicationInvalid)
		}
		found := false
		for _, site := range catalog.Sites {
			if site.ID == review.SiteID && site.ProviderKind == "relay" && sameSiteOrigin(row.SiteURL, site.Website) {
				found = true
				break
			}
		}
		if row.Kind != "relay" || !found {
			return Application{}, fmt.Errorf("%w：请先由中台采集并发布，再关联同官网的正式中转站", ErrApplicationInvalid)
		}
	} else {
		review.SiteID = ""
	}
	return store.ReviewApplication(ctx, id, review, actor)
}

func applicationJSON(a ApplicationInput, state ApplicationState) ([]byte, []byte, error) {
	input, err := json.Marshal(a)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(state)
	return input, raw, err
}
