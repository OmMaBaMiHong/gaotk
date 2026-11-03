package tokencenter

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestPostgresApplicationContactReviewAndRestart(t *testing.T) {
	dsn := os.Getenv("TOKENCENTER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated TokenCenter test database not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	open := func() *sql.DB {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		return db
	}
	db := open()
	var database string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database); err != nil || database != "tokencenter_test" {
		db.Close()
		t.Fatal("requires dedicated tokencenter_test database", err)
	}
	schema := fmt.Sprintf("tokencenter_application_test_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	defer func() {
		cleanup := open()
		defer cleanup.Close()
		cleanup.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
	}()
	setPath := func(db *sql.DB) {
		if _, err := db.ExecContext(ctx, `SET search_path TO `+schema); err != nil {
			t.Fatal(err)
		}
	}
	setPath(db)
	store := NewSQLStore(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := testService(t, store, "")
	gin.SetMode(gin.TestMode)
	router := func(s *Service) *gin.Engine {
		r := gin.New()
		RegisterRoutes(r.Group("/api/v1"), r.Group("/api/v1/admin", DevTokenAuth("test-admin")), s)
		return r
	}
	r := router(service)
	do := func(method, path, body string, admin bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if admin {
			req.Header.Set("Authorization", "Bearer test-admin")
		}
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	submitted := do("POST", "/api/v1/tokencenter/applications", `{"kind":"relay","name":"Local test lead","siteUrl":"https://relay.example","pricingUrl":"https://relay.example/api/pricing","contact":"private@example.invalid","message":"private acceptance inquiry"}`, false)
	if submitted.Code != 201 {
		t.Fatal(submitted.Code, submitted.Body)
	}
	var receipt ApplicationReceipt
	if err := json.Unmarshal(submitted.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt.Receipt) != 64 || strings.Contains(submitted.Body.String(), "private") {
		t.Fatal("receipt missing or private inquiry leaked", submitted.Body)
	}
	var hash string
	var rowCount int
	if err := db.QueryRowContext(ctx, `SELECT receipt_hash FROM tokencenter_applications`).Scan(&hash); err != nil || hash == receipt.Receipt || hash != receiptHash(receipt.Receipt) {
		t.Fatal("receipt not stored as digest", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tokencenter_catalog`).Scan(&rowCount); err != nil || rowCount != 0 {
		t.Fatal("contact inquiry entered public catalog", err)
	}
	if out := do("GET", "/api/v1/admin/tokencenter/applications", "", false); out.Code != 401 {
		t.Fatal("private inbox readable anonymously", out.Code)
	}
	out := do("GET", "/api/v1/admin/tokencenter/applications", "", true)
	var page ApplicationPage
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0].Contact != "private@example.invalid" {
		t.Fatal("admin cannot process inquiry", out.Body)
	}
	id := page.Items[0].ID
	path := fmt.Sprintf("/api/v1/admin/tokencenter/applications/%d/review", id)
	if out := do("POST", path, `{"revision":1,"status":"accepted","note":"已登记线索，等待中台报价采集。"}`, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	if out := do("POST", path, `{"revision":1,"status":"declined","note":"stale"}`, true); out.Code != 409 {
		t.Fatal("stale review accepted", out.Code, out.Body)
	}
	if out := do("POST", path, `{"revision":2,"status":"published","note":"cannot publish missing catalog","siteId":"1"}`, true); out.Code != 422 {
		t.Fatal("missing public source marked published", out.Code, out.Body)
	}
	db.Close()
	db = open()
	defer db.Close()
	setPath(db)
	r = router(testService(t, NewSQLStore(db), ""))
	status := do("POST", "/api/v1/tokencenter/applications/status", `{"receipt":"`+receipt.Receipt+`"}`, false)
	if status.Code != 200 || strings.Contains(status.Body.String(), "private") || !strings.Contains(status.Body.String(), "accepted") {
		t.Fatal("restart lost status or exposed private inquiry", status.Code, status.Body)
	}
	if out := do("POST", "/api/v1/tokencenter/applications/status", `{"receipt":"`+strings.Repeat("0", 64)+`"}`, false); out.Code != 404 {
		t.Fatal("another visitor read private status", out.Code, out.Body)
	}
	stored, err := NewSQLStore(db).ApplicationByID(ctx, id)
	if err != nil || len(stored.History) != 1 || stored.History[0].Status != "accepted" {
		t.Fatal("review audit not persistent", stored, err)
	}
}
