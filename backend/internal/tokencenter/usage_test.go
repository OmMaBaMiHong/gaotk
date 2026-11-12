package tokencenter

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"
)

func TestPlatformDirectoryUsesPublishedEntities(t *testing.T) {
	c := Catalog{Sites: []Site{{ID: "1", EntityID: "1", Name: "Official", ProviderKind: "manufacturer"}, {ID: "2", EntityID: "2", Name: "Relay", ProviderKind: "relay"}}, SubscriptionMerchants: []MerchantOffer{{MerchantID: "3", Name: "Store", MerchantURL: "https://store.example"}, {MerchantID: "3", Name: "Store"}, {MerchantID: "2", Name: "Relay store"}}, Models: []Model{{ID: "model", DeveloperID: "Official"}}}
	d := projectPlatformDirectory(c)
	if d.Total != 3 || len(d.Groups[0].Items) != 1 || len(d.Groups[2].Items) != 1 || len(d.Groups[3].Items) != 1 {
		t.Fatalf("duplicate offers or models counted as platforms: %+v", d)
	}
	if d.Groups[3].Items[0].Href != "https://store.example" {
		t.Fatal("merchant destination lost")
	}
	c.SubscriptionMerchants = append(c.SubscriptionMerchants, MerchantOffer{Name: "No identity"})
	c.Resources = []ResourceOffer{{Provider: "Unknown identity"}}
	d = projectPlatformDirectory(c)
	if d.Total != 3 || d.UnresolvedOfferCount != 2 {
		t.Fatalf("unresolved identity silently fabricated: %+v", d)
	}
}

func TestPostgresUsageConcurrentClicksAndRestart(t *testing.T) {
	dsn := os.Getenv("TOKENCENTER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated TokenCenter test database not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	control, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	var dbName string
	if err = control.QueryRowContext(ctx, "SELECT current_database()").Scan(&dbName); err != nil || dbName != "tokencenter_test" {
		t.Fatal("requires dedicated tokencenter_test", err)
	}
	schema := fmt.Sprintf("tokencenter_usage_test_%d", time.Now().UnixNano())
	if _, err = control.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer control.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	q.Set("search_path", schema)
	parsed.RawQuery = q.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(5)
	store := NewSQLStore(db)
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r.Group("/api/v1"), r.Group("/api/v1/admin"), testService(t, store, ""))
	request := func(kind string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/tokencenter/usage", bytes.NewBufferString(`{"kind":"`+kind+`"}`))
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	if out := request("pageview"); out.Code != 400 {
		t.Fatalf("invalid category accepted: %d", out.Code)
	}
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if out := request([]string{"board", "model", "platform"}[i%3]); out.Code != 200 {
				t.Errorf("click %d: %d", i, out.Code)
			}
		}(i)
	}
	wg.Wait()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := NewSQLStore(reopened).ServiceUsage(ctx)
	if err != nil || got.Total != 60 || got.Boards != 20 || got.Models != 20 || got.Platforms != 20 {
		b, _ := json.Marshal(got)
		t.Fatalf("click loss or reset after restart: %s %v", b, err)
	}
}
