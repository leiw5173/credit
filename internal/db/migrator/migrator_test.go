package migrator

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func postgresErrorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func freshConnectionDB(db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true})
}

func TestDuplicateMigrationVersionFails(t *testing.T) {
	if err := ValidateVersions([]string{"0001_credis_ledger.up.sql", "0002_a.up.sql", "0002_b.up.sql"}); err == nil {
		t.Fatal("duplicate migration version accepted")
	}
}

func TestMigrationVersionGapFails(t *testing.T) {
	if err := ValidateVersions([]string{"0001_credis_ledger.up.sql", "0003_next.up.sql"}); err == nil {
		t.Fatal("migration version gap accepted")
	}
}

func TestLedgerMigration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	schema := fmt.Sprintf("ledger_migration_test_%d", time.Now().UnixNano())
	if !strings.HasPrefix(schema, "ledger_migration_test_") {
		t.Fatal("unexpected test schema name")
	}

	if err := db.Connection(func(tx *gorm.DB) error {
		if err := freshConnectionDB(tx).Exec("CREATE SCHEMA " + schema).Error; err != nil {
			return err
		}
		defer freshConnectionDB(tx).Exec("DROP SCHEMA " + schema + " CASCADE")
		if err := freshConnectionDB(tx).Exec("SET search_path TO " + schema).Error; err != nil {
			return err
		}

		if err := ApplyVersioned(freshConnectionDB(tx)); err != nil {
			return err
		}
		if err := ApplyVersioned(freshConnectionDB(tx)); err != nil {
			return err
		}

		var count int64
		if err := freshConnectionDB(tx).Table("schema_migrations").Where("version = ?", 1).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("migration applied %d times", count)
		}

		var accountID int64
		if err := freshConnectionDB(tx).Raw("INSERT INTO ledger_accounts (forum_user_id) VALUES (1001) RETURNING id").Scan(&accountID).Error; err != nil {
			return err
		}
		if err := freshConnectionDB(tx).Exec(`INSERT INTO ledger_entries
			(account_id, action, available_delta, source_kind, source_id, idempotency_key, occurred_at)
			VALUES (?, 'earn', 100, 'test', 'source-1', 'key-1', now())`, accountID).Error; err != nil {
			return err
		}
		if err := freshConnectionDB(tx).Exec("UPDATE ledger_entries SET available_delta = 99").Error; postgresErrorCode(err) != "P0001" {
			return fmt.Errorf("ledger entry update error code = %q, want P0001", postgresErrorCode(err))
		}
		if err := freshConnectionDB(tx).Exec("DELETE FROM ledger_entries").Error; postgresErrorCode(err) != "P0001" {
			return fmt.Errorf("ledger entry delete error code = %q, want P0001", postgresErrorCode(err))
		}
		if err := freshConnectionDB(tx).Exec(`INSERT INTO ledger_entries
			(account_id, action, available_delta, source_kind, source_id, idempotency_key, occurred_at)
			VALUES (?, 'earn', 100, 'test', 'source-2', 'key-1', now())`, accountID).Error; postgresErrorCode(err) != "23505" {
			return fmt.Errorf("duplicate ledger idempotency key error code = %q, want 23505", postgresErrorCode(err))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
