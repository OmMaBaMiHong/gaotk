package tokencenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func businessTestDatabase(t *testing.T) (*SQLStore, string) {
	t.Helper()
	dsn := os.Getenv("TOKENCENTER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated TokenCenter test database not configured")
	}
	control, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err = control.QueryRow("SELECT current_database()").Scan(&name); err != nil || name != "tokencenter_test" {
		control.Close()
		t.Fatal("requires dedicated tokencenter_test database", err)
	}
	schema := fmt.Sprintf("tokencenter_business_test_%d", time.Now().UnixNano())
	if _, err = control.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(5)
	t.Cleanup(func() { db.Close(); control.Exec("DROP SCHEMA " + schema + " CASCADE"); control.Close() })
	s := NewSQLStore(db)
	if err = s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, u.String()
}
func TestPostgresBusinessLifecycleConcurrentClaimsAndAdvertising(t *testing.T) {
	s, dsn := businessTestDatabase(t)
	ctx := context.Background()
	u1 := BusinessIdentity{ID: 11, Issuer: "shared-accounts", Email: "one@example.test"}
	u2 := BusinessIdentity{ID: 12, Issuer: "shared-accounts", Email: "two@example.test"}
	profile := MerchantProfile{Name: "Acceptance merchant", SiteURL: "https://merchant.example", Contact: "private contact", Message: "local test"}
	m1, err := s.SaveMerchant(ctx, u1, profile, 0)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := s.SaveMerchant(ctx, u2, profile, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveMerchant(ctx, u1, profile, 0); !errors.Is(err, ErrBusinessConflict) {
		t.Fatal("duplicate merchant replaced", err)
	}
	platform := PlatformItem{ID: "published-platform-1", Name: "Published platform", Href: "https://platform.example"}
	evidence := ClaimEvidence{Contact: "private contact", ProofURL: "https://platform.example/proof", Statement: "owned by merchant"}
	if _, err = s.SubmitClaim(ctx, m1.ID, platform, evidence, "member"); !errors.Is(err, ErrBusinessInvalid) {
		t.Fatal("unreviewed merchant claimed platform", err)
	}
	m1, err = s.ReviewMerchant(ctx, m1.ID, BusinessReview{Revision: 1, Status: "approved", Note: "verified"}, "operator:1")
	if err != nil {
		t.Fatal(err)
	}
	m2, err = s.ReviewMerchant(ctx, m2.ID, BusinessReview{Revision: 1, Status: "approved", Note: "verified"}, "operator:1")
	if err != nil {
		t.Fatal(err)
	}
	c1, err := s.SubmitClaim(ctx, m1.ID, platform, evidence, "member:11")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := s.SubmitClaim(ctx, m2.ID, platform, evidence, "member:12")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitClaim(ctx, m1.ID, platform, evidence, "member:11"); !errors.Is(err, ErrBusinessConflict) {
		t.Fatal("duplicate pending claim", err)
	}
	var wg sync.WaitGroup
	out := make(chan PlatformClaim, 2)
	failures := make(chan error, 2)
	for _, c := range []PlatformClaim{c1, c2} {
		wg.Add(1)
		go func(c PlatformClaim) {
			defer wg.Done()
			saved, e := s.ReviewClaim(ctx, c.ID, BusinessReview{Revision: 1, Status: "approved", Note: "verified ownership"}, "operator:1")
			if e == nil {
				out <- saved
			} else {
				failures <- e
			}
		}(c)
	}
	wg.Wait()
	close(out)
	close(failures)
	if len(out) != 1 || len(failures) != 1 {
		t.Fatal("concurrent platform ownership was not unique")
	}
	winner := <-out
	if e := <-failures; !errors.Is(e, ErrBusinessConflict) {
		t.Fatal(e)
	}
	owner, other, u := m1, m2, u1
	if winner.MerchantID == m2.ID {
		owner, other, u = m2, m1, u2
	}
	slots, err := s.Slots(ctx, 0)
	if err != nil || len(slots.Items) != 1 {
		t.Fatal(slots, err)
	}
	slot := slots.Items[0]
	creative := AdCreative{Brand: "Acceptance brand", Title: "Local test advertisement", Description: "No billing", DestinationURL: "https://merchant.example", ButtonText: "View"}
	in := AdCampaignInput{ClaimID: winner.ID, SlotID: slot.ID, Creative: creative, StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour), Submit: true}
	if _, err = s.SaveCampaign(ctx, other.ID, 0, in, "forged-owner"); !errors.Is(err, ErrBusinessForbidden) {
		t.Fatal("cross-merchant claim accepted", err)
	}
	ad, err := s.SaveCampaign(ctx, owner.ID, 0, in, "member")
	if err != nil {
		t.Fatal(err)
	}
	if visible, _ := s.PublicCampaign(ctx, slot.Code); visible != nil {
		t.Fatal("pending ad exposed")
	}
	ad, err = s.ReviewCampaign(ctx, ad.ID, BusinessReview{Revision: ad.Revision, Status: "approved", Note: "creative checked"}, "operator:1")
	if err != nil {
		t.Fatal(err)
	}
	visible, err := s.PublicCampaign(ctx, slot.Code)
	if err != nil || visible == nil || visible.ID != ad.ID {
		t.Fatal("approved ad not visible", err)
	}
	t.Run("HTTP ownership cannot be supplied by request", func(t *testing.T) {
		otherUser := u1
		if u.ID == u1.ID {
			otherUser = u2
		}
		gin.SetMode(gin.TestMode)
		router := gin.New()
		// Authentication is covered separately by the AccountBridge tests. These
		// requests test that the handler uses that identity, never caller IDs.
		member := router.Group("/api/v1", func(c *gin.Context) { c.Set(businessIdentityKey, otherUser); c.Next() })
		RegisterBusinessRoutes(router.Group("/api/v1"), router.Group("/api/v1/admin"), member, &Service{store: s})
		body, _ := json.Marshal(in)
		for _, tc := range []struct {
			method, path, body string
			want               int
		}{
			{"PUT", fmt.Sprintf("/campaigns/%d", ad.ID), "{}", 403},
			{"POST", fmt.Sprintf("/campaigns/%d/pause", ad.ID), "{}", 403},
			{"POST", "/campaigns", string(body), 403},
			{"GET", fmt.Sprintf("/campaigns?merchantId=%d", owner.ID), "", 200},
			{"GET", fmt.Sprintf("/merchant?userId=%d", u.ID), "", 200},
		} {
			req := httptest.NewRequest(tc.method, "/api/v1/tokencenter/business"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-User-Id", fmt.Sprint(u.ID))
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != tc.want {
				t.Fatalf("%s %s: %d %s", tc.method, tc.path, out.Code, out.Body)
			}
			if tc.method == "GET" && strings.HasPrefix(tc.path, "/campaigns") {
				var page BusinessPage[AdCampaign]
				if err := json.Unmarshal(out.Body.Bytes(), &page); err != nil || len(page.Items) != 0 {
					t.Fatal("foreign campaigns leaked", out.Body)
				}
			}
			if tc.method == "GET" && strings.HasPrefix(tc.path, "/merchant") {
				var result struct {
					Merchant BusinessMerchant `json:"merchant"`
				}
				if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil || result.Merchant.ID != other.ID {
					t.Fatal("forged merchant identity", out.Body)
				}
			}
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest("GET", "/api/v1/tokencenter/business/ads/"+slot.Code, nil))
		if out.Code != 200 || strings.Contains(out.Body.String(), "private contact") || strings.Contains(out.Body.String(), "history") || strings.Contains(out.Body.String(), "merchantId") {
			t.Fatal("public creative exposed private business fields", out.Body)
		}
	})
	duplicate, err := s.SaveCampaign(ctx, owner.ID, 0, in, "member")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReviewCampaign(ctx, duplicate.ID, BusinessReview{Revision: 1, Status: "approved", Note: "overlapping"}, "operator:1"); !errors.Is(err, ErrBusinessConflict) {
		t.Fatal("double booked slot", err)
	}
	slot.Enabled = false
	slot, err = s.SaveSlot(ctx, slot.ID, slot, "operator:1")
	if err != nil {
		t.Fatal(err)
	}
	if visible, _ = s.PublicCampaign(ctx, slot.Code); visible != nil {
		t.Fatal("disabled slot visible")
	}
	page, err := s.Campaigns(ctx, owner.ID, "approved", 0)
	if err != nil || page.Items[0].DeliveryStatus != "slot_disabled" {
		t.Fatal("delivery state inconsistent", err)
	}
	slot.Enabled = true
	slot, err = s.SaveSlot(ctx, slot.ID, slot, "operator:1")
	if err != nil {
		t.Fatal(err)
	}
	in.Revision = ad.Revision
	in.Creative.Title = "Changed creative"
	ad, err = s.SaveCampaign(ctx, owner.ID, ad.ID, in, "member")
	if err != nil {
		t.Fatal(err)
	}
	if visible, _ = s.PublicCampaign(ctx, slot.Code); visible != nil {
		t.Fatal("edited creative bypassed review")
	}
	ad, err = s.ReviewCampaign(ctx, ad.ID, BusinessReview{Revision: ad.Revision, Status: "approved", Note: "new creative checked"}, "operator:1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReviewClaim(ctx, winner.ID, BusinessReview{Revision: winner.Revision, Status: "revoked", Note: "ownership withdrawn"}, "operator:1"); err != nil {
		t.Fatal(err)
	}
	if visible, _ = s.PublicCampaign(ctx, slot.Code); visible != nil {
		t.Fatal("revoked owner still advertising")
	}
	paused, _ := s.Campaign(ctx, ad.ID)
	if paused.Status != "paused" {
		t.Fatal("missing revocation cascade")
	}
	if _, err = s.ReviewCampaign(ctx, ad.ID, BusinessReview{Revision: paused.Revision, Status: "approved", Note: "invalid restore"}, "operator:1"); !errors.Is(err, ErrBusinessInvalid) {
		t.Fatal("revoked owner restored advertising", err)
	}
	owner, err = s.ReviewMerchant(ctx, owner.ID, BusinessReview{Revision: owner.Revision, Status: "suspended", Note: "test complete"}, "operator:1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveMerchant(ctx, u, profile, owner.Revision); !errors.Is(err, ErrBusinessInvalid) {
		t.Fatal("merchant bypassed suspension", err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	restored, err := NewSQLStore(db).MerchantForAccount(ctx, u)
	if err != nil || restored.Status != "suspended" || len(restored.History) != 3 {
		t.Fatal("state/history lost after reopen", restored, err)
	}
	var catalogCount int
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM tokencenter_catalog").Scan(&catalogCount); err != nil || catalogCount != 0 {
		t.Fatal("business wrote catalog", err)
	}
	usage, err := s.ServiceUsage(ctx)
	if err != nil || usage.Total != 0 {
		t.Fatal("business inflated service counter", err)
	}
}
