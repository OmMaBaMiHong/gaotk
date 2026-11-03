package tokencenter

import (
	"context"
	"database/sql"
	"time"
)

// ProvideService reuses the main application's migrated SQL pool.
func ProvideService(db *sql.DB) (*Service, func(), error) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		return nil, nil, err
	}
	service, err := NewService(NewSQLStore(db), cfg)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := service.Seed(ctx); err != nil {
		return nil, nil, err
	}
	service.Start(context.Background())
	return service, service.Stop, nil
}
