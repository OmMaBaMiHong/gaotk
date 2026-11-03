package tokencenter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestApplicationEntryIsPublicButInboxRequiresExistingAdminAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), router.Group("/api/v1/admin", DevTokenAuth("test-admin")), testService(t, &memoryStore{}, ""))
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/v1/tokencenter/applications", `{}`, 400},
		{"POST", "/api/v1/tokencenter/applications/status", `{}`, 400},
		{"GET", "/api/v1/admin/tokencenter/applications", "", 401},
		{"POST", "/api/v1/admin/tokencenter/applications/1/review", `{}`, 401},
	} {
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body)))
		if out.Code != tc.status {
			t.Fatalf("%s %s: got %d, want %d: %s", tc.method, tc.path, out.Code, tc.status, out.Body)
		}
	}
}

type applicationMemoryStore struct {
	memoryStore
	rows   map[int64]Application
	hashes map[string]int64
}

func (s *applicationMemoryStore) CreateApplication(_ context.Context, in ApplicationInput, hash string) (Application, error) {
	if s.rows == nil {
		s.rows = map[int64]Application{}
		s.hashes = map[string]int64{}
	}
	a := Application{ID: int64(len(s.rows) + 1), ApplicationInput: in, ApplicationState: ApplicationState{Status: "pending", PublicNote: "申请已保存，等待处理。", History: []ApplicationEvent{}}, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	s.rows[a.ID] = a
	s.hashes[hash] = a.ID
	return a, nil
}
func (s *applicationMemoryStore) ApplicationByReceipt(ctx context.Context, hash string) (Application, error) {
	return s.ApplicationByID(ctx, s.hashes[hash])
}
func (s *applicationMemoryStore) ApplicationByID(_ context.Context, id int64) (Application, error) {
	a, ok := s.rows[id]
	if !ok {
		return a, ErrApplicationNotFound
	}
	return a, nil
}
func (s *applicationMemoryStore) ListApplications(_ context.Context, status string, before int64) (ApplicationPage, error) {
	return ApplicationPage{}, nil
}
func (s *applicationMemoryStore) ReviewApplication(_ context.Context, id int64, r ApplicationReview, actor string) (Application, error) {
	a := s.rows[id]
	if a.Revision != r.Revision {
		return a, ErrApplicationConflict
	}
	a.Status = r.Status
	a.PublicNote = r.Note
	a.SiteID = r.SiteID
	a.Revision++
	a.History = append(a.History, ApplicationEvent{At: time.Now().UTC(), Status: r.Status, Note: r.Note, Actor: actor})
	s.rows[id] = a
	return a, nil
}

func TestApplicationReceiptIsPrivateAndReviewNeverWritesCatalog(t *testing.T) {
	ctx := context.Background()
	store := &applicationMemoryStore{}
	s := testService(t, store, "")
	in := ApplicationInput{Kind: "relay", Name: "Catalog Lead", SiteURL: "https://relay.example", PricingURL: "https://relay.example/api/pricing", Contact: "private@example.invalid", Message: "Private contact inquiry"}
	receipt, err := s.SubmitApplication(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Receipt) != 64 || receipt.Status != "pending" {
		t.Fatalf("bad receipt: %+v", receipt)
	}
	encoded, _ := json.Marshal(receipt)
	if strings.Contains(string(encoded), in.Contact) || strings.Contains(string(encoded), in.Message) {
		t.Fatal("private inquiry leaked into public receipt")
	}
	if _, err = s.ApplicationStatus(ctx, strings.Repeat("0", 64)); !errors.Is(err, ErrApplicationNotFound) {
		t.Fatal("another receipt read inquiry", err)
	}
	if _, err = s.ReviewApplication(ctx, 1, ApplicationReview{Revision: 1, Status: "accepted", Note: "已登记采集线索，待报价采集。"}, "admin:7"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Catalog(ctx); !errors.Is(err, ErrEmpty) {
		t.Fatal("accepting inquiry published a catalog", err)
	}
	status, err := s.ApplicationStatus(ctx, receipt.Receipt)
	if err != nil || status.Status != "accepted" {
		t.Fatal(status, err)
	}
	if _, err = s.ReviewApplication(ctx, 1, ApplicationReview{Revision: 1, Status: "contacted", Note: "stale"}, "admin:7"); !errors.Is(err, ErrApplicationConflict) {
		t.Fatal("stale review overwrote new state", err)
	}
}

func TestApplicationReviewCannotPublishDemoCatalog(t *testing.T) {
	ctx := context.Background()
	store := &applicationMemoryStore{}
	s := testService(t, store, "")
	c := fixture(t)
	c.Sites[0].ProviderKind = "relay"
	if _, err := s.Import(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitApplication(ctx, ApplicationInput{Kind: "relay", Name: "Test Lead", SiteURL: c.Sites[0].Website, Contact: "private@example.invalid"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReviewApplication(ctx, 1, ApplicationReview{Revision: 1, Status: "published", Note: "Should never be public from demo", SiteID: c.Sites[0].ID}, "admin:7"); !errors.Is(err, ErrApplicationInvalid) {
		t.Fatalf("demo catalog was marked formally published: %v", err)
	}
}
