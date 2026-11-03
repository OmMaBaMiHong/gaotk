// TokenCenter's local launcher uses the same directory service/store/routes as sub2api.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/tokencenter"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	databaseURL, token := os.Getenv("TOKENCENTER_DATABASE_URL"), os.Getenv("TOKENCENTER_ADMIN_TOKEN")
	if databaseURL == "" {
		return errors.New("TOKENCENTER_DATABASE_URL is required; use a dedicated TokenCenter database in the shared PostgreSQL instance")
	}
	if token == "" {
		return errors.New("TOKENCENTER_ADMIN_TOKEN is required")
	}
	addr := os.Getenv("TOKENCENTER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8086"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("TOKENCENTER_ADDR must be host:port")
	}
	if port != "8086" {
		return errors.New("local TokenCenter launcher must use port 8086 to protect existing services")
	}
	cfg, err := tokencenter.ConfigFromEnv()
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return errors.New("could not open TOKENCENTER_DATABASE_URL")
	}
	defer db.Close()
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return errors.New("TokenCenter PostgreSQL connection failed; check dedicated database configuration")
	}
	store := tokencenter.NewSQLStore(db)
	if err = store.Migrate(ctx); err != nil {
		return errors.New("TokenCenter catalog migration failed")
	}
	service, err := tokencenter.NewService(store, cfg)
	if err != nil {
		return err
	}
	if err = service.Seed(ctx); err != nil {
		return errors.New("TokenCenter demo seed failed")
	}
	lifecycle, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	service.Start(lifecycle)
	defer service.Stop()
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	_ = router.SetTrustedProxies(nil)
	public := router.Group("/api/v1")
	admin := router.Group("/api/v1/admin", tokencenter.DevTokenAuth(token))
	tokencenter.RegisterRoutes(public, admin, service)
	server := &http.Server{Addr: addr, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: cfg.Timeout + 5*time.Second, IdleTimeout: 60 * time.Second}
	failures := make(chan error, 1)
	go func() { failures <- server.ListenAndServe() }()
	log.Printf("TokenCenter listening on %s (configured source: %t, demo seed enabled: %t)", addr, cfg.SourceURL != "", cfg.SeedDemo)
	select {
	case err = <-failures:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-lifecycle.Done():
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		return server.Shutdown(shutdown)
	}
	return nil
}
