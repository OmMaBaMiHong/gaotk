package tokencenter

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// ProvideService owns the optional catalog database pool. It deliberately takes
// no account database dependency and never falls back to the main SQL pool.
func ProvideService() (*Service, func(), error) {
	return provideService(sql.Open)
}

func provideService(openDB func(string, string) (*sql.DB, error)) (*Service, func(), error) {
	databaseURL := strings.TrimSpace(os.Getenv("TOKENCENTER_DATABASE_URL"))
	if databaseURL == "" {
		return nil, func() {}, nil
	}
	cfg, err := ConfigFromEnv()
	if err != nil {
		return nil, nil, err
	}
	db, err := openDB("postgres", databaseURL)
	if err != nil {
		return nil, nil, errors.New("could not open TOKENCENTER_DATABASE_URL")
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	initialized := false
	defer func() {
		if !initialized {
			_ = db.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, nil, errors.New("TokenCenter PostgreSQL connection failed; check dedicated database configuration")
	}
	store := NewSQLStore(db)
	if err := store.Migrate(ctx); err != nil {
		return nil, nil, errors.New("TokenCenter catalog migration failed")
	}
	service, err := NewService(store, cfg)
	if err != nil {
		return nil, nil, err
	}
	if err := service.Seed(ctx); err != nil {
		return nil, nil, errors.New("TokenCenter demo seed failed")
	}
	service.Start(context.Background())
	initialized = true
	return service, func() {
		service.Stop()
		_ = db.Close()
	}, nil
}
