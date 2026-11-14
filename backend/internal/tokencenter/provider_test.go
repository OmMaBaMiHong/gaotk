package tokencenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestProvideServiceUnconfiguredIsDisabled(t *testing.T) {
	t.Setenv("TOKENCENTER_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "postgres://account-service/main-accounts")
	// Unconfigured optional modules must not block account-service startup, even
	// if unrelated TokenCenter configuration was left in the environment.
	t.Setenv("TOKENCENTER_SYNC_INTERVAL", "invalid")
	service, cleanup, err := ProvideService()
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("unconfigured TokenCenter blocked account startup: %v", err)
	}
	if service != nil {
		t.Fatal("TokenCenter enabled without its own database URL")
	}
}

func configureProviderTest(t *testing.T, seed bool) string {
	t.Helper()
	const databaseURL = "postgres://catalog-user@catalog-host/catalog-data"
	t.Setenv("TOKENCENTER_DATABASE_URL", databaseURL)
	t.Setenv("TOKENCENTER_SOURCE_URL", "")
	t.Setenv("TOKENCENTER_SOURCE_TOKEN", "")
	t.Setenv("TOKENCENTER_SYNC_INTERVAL", "")
	t.Setenv("TOKENCENTER_SYNC_TIMEOUT", "")
	if seed {
		t.Setenv("TOKENCENTER_SEED_DEMO", "true")
	} else {
		t.Setenv("TOKENCENTER_SEED_DEMO", "false")
	}
	return databaseURL
}

func TestProvideServiceMigratesAndOwnsDedicatedDatabase(t *testing.T) {
	databaseURL := configureProviderTest(t, true)
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectPing()
	mock.ExpectExec(regexp.QuoteMeta(catalogSchema)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`ON CONFLICT \(id\) DO NOTHING`).WithArgs(sqlmock.AnyArg(), "", "", "", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	service, cleanup, err := provideService(func(driver, dsn string) (*sql.DB, error) {
		if driver != "postgres" || dsn != databaseURL {
			t.Fatal("provider did not open the explicit dedicated PostgreSQL URL")
		}
		return db, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if service == nil || cleanup == nil {
		t.Fatal("configured catalog missing service or pool cleanup")
	}
	defer cleanup()
	// Read through the initialized service, proving it uses the same dedicated
	// pool that was migrated and seeded rather than an account pool.
	demo, err := DemoCatalog()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(demo)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT catalog, source_url, etag, last_modified, access_status FROM tokencenter_catalog WHERE id = 1`)).WillReturnRows(sqlmock.NewRows([]string{"catalog", "source_url", "etag", "last_modified", "access_status"}).AddRow(data, "", "", "", 0))
	got, err := service.Catalog(context.Background())
	if err != nil || got.Version != demo.Version {
		t.Fatalf("initialized service did not read its dedicated pool: %v", err)
	}
	mock.ExpectClose()
	cleanup()
	cleanup()
	if err := db.Ping(); err == nil {
		t.Fatal("provider cleanup did not close its own database pool")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProvideServiceClosesPoolOnInitializationFailure(t *testing.T) {
	for _, stage := range []string{"connection", "migration", "seed"} {
		t.Run(stage, func(t *testing.T) {
			configureProviderTest(t, stage == "seed")
			db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			failure := errors.New("private driver error")
			if stage == "connection" {
				mock.ExpectPing().WillReturnError(failure)
			} else {
				mock.ExpectPing()
				migration := mock.ExpectExec(regexp.QuoteMeta(catalogSchema))
				if stage == "migration" {
					migration.WillReturnError(failure)
				} else {
					migration.WillReturnResult(sqlmock.NewResult(0, 0))
					mock.ExpectExec(`ON CONFLICT \(id\) DO NOTHING`).WillReturnError(failure)
				}
			}
			mock.ExpectClose()
			service, cleanup, err := provideService(func(string, string) (*sql.DB, error) { return db, nil })
			if cleanup != nil {
				defer cleanup()
			}
			if err == nil || service != nil {
				t.Fatal("database initialization failure was ignored")
			}
			if strings.Contains(err.Error(), failure.Error()) {
				t.Fatal("private database error leaked from provider")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProvideServiceConfiguredDatabaseFailureDoesNotFallback(t *testing.T) {
	const databaseURL = "postgres://catalog-user:do-not-log@/%zz"
	t.Setenv("TOKENCENTER_DATABASE_URL", databaseURL)
	t.Setenv("TOKENCENTER_SOURCE_URL", "")
	t.Setenv("TOKENCENTER_SOURCE_TOKEN", "")
	t.Setenv("TOKENCENTER_SYNC_INTERVAL", "")
	t.Setenv("TOKENCENTER_SYNC_TIMEOUT", "")
	t.Setenv("TOKENCENTER_SEED_DEMO", "false")
	service, cleanup, err := ProvideService()
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil || service != nil {
		t.Fatal("configured TokenCenter database was ignored instead of failing startup")
	}
	if strings.Contains(err.Error(), "do-not-log") || strings.Contains(err.Error(), databaseURL) {
		t.Fatal("database configuration credentials leaked in startup error")
	}
}
