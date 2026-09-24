package migrator

import (
	"errors"
	"fmt"
	"net/url"
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
		if err := freshConnectionDB(tx).Exec("TRUNCATE ledger_entries").Error; postgresErrorCode(err) != "P0001" {
			return fmt.Errorf("ledger entry truncate error code = %q, want P0001", postgresErrorCode(err))
		}
		var entryCount int64
		if err := freshConnectionDB(tx).Table("ledger_entries").Count(&entryCount).Error; err != nil {
			return err
		}
		if entryCount != 1 {
			return fmt.Errorf("ledger entries after rejected truncate = %d, want 1", entryCount)
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

func roleDSN(t *testing.T, dsn, role, password, schema string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, password)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func TestRuntimeRoleCannotMutateLedger(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UnixNano()
	ownerRole := fmt.Sprintf("ledger_owner_%d", stamp)
	runtimeRole := fmt.Sprintf("ledger_runtime_%d", stamp)
	schema := fmt.Sprintf("ledger_roles_%d", stamp)
	password := fmt.Sprintf("local_%d", stamp)
	for _, statement := range []string{
		fmt.Sprintf("CREATE ROLE \"%s\" LOGIN NOINHERIT PASSWORD '%s'", ownerRole, password),
		fmt.Sprintf("CREATE ROLE \"%s\" LOGIN INHERIT PASSWORD '%s'", runtimeRole, password),
		fmt.Sprintf("CREATE SCHEMA \"%s\" AUTHORIZATION \"%s\"", schema, ownerRole),
	} {
		if err := admin.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	defer admin.Exec(fmt.Sprintf("DROP SCHEMA \"%s\" CASCADE", schema))
	owner, err := gorm.Open(postgres.Open(roleDSN(t, dsn, ownerRole, password, schema)), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(owner, runtimeRole); err != nil {
		t.Fatal(err)
	}
	// A sequence added after the first migration must remain owner-only on rerun.
	if err := owner.Exec("CREATE SEQUENCE future_owner_only_seq").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(owner, runtimeRole); err != nil {
		t.Fatal(err)
	}
	runtime, err := gorm.Open(postgres.Open(roleDSN(t, dsn, runtimeRole, password, schema)), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var inherits bool
	if err := runtime.Raw("SELECT rolinherit FROM pg_roles WHERE rolname = current_user").Scan(&inherits).Error; err != nil || !inherits {
		t.Fatalf("expected default INHERIT login, inherits = %t, err = %v", inherits, err)
	}
	if err := VerifyRuntime(runtime); err != nil {
		t.Fatalf("default INHERIT login without owner membership rejected: %v", err)
	}
	for _, sequence := range []string{"ledger_accounts_id_seq", "future_owner_only_seq"} {
		for _, privilege := range []string{"USAGE", "SELECT"} {
			var granted bool
			if err := runtime.Raw("SELECT has_sequence_privilege(current_user, ?, ?)", sequence, privilege).Scan(&granted).Error; err != nil || granted {
				t.Fatalf("runtime %s on %s = %t, err = %v", privilege, sequence, granted, err)
			}
		}
		if err := runtime.Exec("SELECT nextval('" + sequence + "')").Error; postgresErrorCode(err) != "42501" {
			t.Fatalf("runtime nextval(%s) code = %q, want 42501", sequence, postgresErrorCode(err))
		}
	}
	for _, sequence := range []string{"ledger_entries_id_seq", "audit_logs_id_seq", "outbox_events_id_seq", "users_id_seq"} {
		var granted bool
		if err := runtime.Raw("SELECT has_sequence_privilege(current_user, ?, 'USAGE')", sequence).Scan(&granted).Error; err != nil || !granted {
			t.Fatalf("runtime USAGE on %s = %t, err = %v", sequence, granted, err)
		}
	}
	assertVerifyFails := func(label string) {
		t.Helper()
		if err := VerifyRuntime(runtime); err == nil {
			t.Fatalf("runtime accepted %s", label)
		}
	}
	if err := admin.Exec(fmt.Sprintf("GRANT \"%s\" TO \"%s\"", ownerRole, runtimeRole)).Error; err != nil {
		t.Fatal(err)
	}
	assertVerifyFails("migration-owner membership")
	if err := runtime.Exec(fmt.Sprintf("SET ROLE \"%s\"", ownerRole)).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Exec("RESET ROLE").Error; err != nil {
		t.Fatal(err)
	}
	if err := admin.Exec(fmt.Sprintf("REVOKE \"%s\" FROM \"%s\"", ownerRole, runtimeRole)).Error; err != nil {
		t.Fatal(err)
	}
	if err := owner.Exec("ALTER TABLE ledger_entries DISABLE TRIGGER ledger_entries_immutable").Error; err != nil {
		t.Fatal(err)
	}
	assertVerifyFails("disabled immutable trigger")
	if err := owner.Exec("ALTER TABLE ledger_entries ENABLE TRIGGER ledger_entries_immutable").Error; err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{"ledger_entries_immutable", "ledger_entries_no_truncate"} {
		if err := owner.Exec("ALTER TABLE ledger_entries ENABLE REPLICA TRIGGER " + trigger).Error; err != nil {
			t.Fatal(err)
		}
		assertVerifyFails("REPLICA-only " + trigger)
		if err := owner.Exec("ALTER TABLE ledger_entries ENABLE TRIGGER " + trigger).Error; err != nil {
			t.Fatal(err)
		}
		if err := VerifyRuntime(runtime); err != nil {
			t.Fatalf("restored origin trigger %s rejected: %v", trigger, err)
		}
	}
	if err := owner.Exec("DROP TRIGGER ledger_entries_immutable ON ledger_entries").Error; err != nil {
		t.Fatal(err)
	}
	assertVerifyFails("missing immutable trigger")
	if err := owner.Exec("CREATE TRIGGER ledger_entries_immutable BEFORE UPDATE OR DELETE ON ledger_entries FOR EACH ROW EXECUTE FUNCTION prevent_ledger_entry_mutation()").Error; err != nil {
		t.Fatal(err)
	}
	if err := owner.Exec("ALTER TABLE audit_logs RENAME TO audit_logs_missing").Error; err != nil {
		t.Fatal(err)
	}
	assertVerifyFails("missing audit_logs")
	if err := owner.Exec("ALTER TABLE audit_logs_missing RENAME TO audit_logs").Error; err != nil {
		t.Fatal(err)
	}
	if err := owner.Exec("ALTER TABLE outbox_events RENAME TO outbox_events_missing").Error; err != nil {
		t.Fatal(err)
	}
	assertVerifyFails("missing outbox_events")
	if err := owner.Exec("ALTER TABLE outbox_events_missing RENAME TO outbox_events").Error; err != nil {
		t.Fatal(err)
	}
	if err := owner.Exec("UPDATE schema_migrations SET name = 'wrong.sql' WHERE version = 1").Error; err != nil {
		t.Fatal(err)
	}
	assertVerifyFails("wrong migration name")
	if err := owner.Exec("UPDATE schema_migrations SET name = '0001_credis_ledger.up.sql' WHERE version = 1").Error; err != nil {
		t.Fatal(err)
	}
	if err := owner.Exec(fmt.Sprintf("REVOKE INSERT ON ledger_entries FROM \"%s\"", runtimeRole)).Error; err != nil {
		t.Fatal(err)
	}
	assertVerifyFails("SELECT-only ledger privilege")
	if err := owner.Exec(fmt.Sprintf("GRANT INSERT ON ledger_entries TO \"%s\"", runtimeRole)).Error; err != nil {
		t.Fatal(err)
	}
	if err := owner.Exec(fmt.Sprintf("REVOKE SELECT ON ledger_entries FROM \"%s\"", runtimeRole)).Error; err != nil {
		t.Fatal(err)
	}
	assertVerifyFails("INSERT-only ledger privilege")
	if err := owner.Exec(fmt.Sprintf("GRANT SELECT ON ledger_entries TO \"%s\"", runtimeRole)).Error; err != nil {
		t.Fatal(err)
	}
	if err := VerifyRuntime(runtime); err != nil {
		t.Fatal(err)
	}
	var systemConfigCount int64
	if err := runtime.Table("system_configs").Count(&systemConfigCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Exec("INSERT INTO system_configs (key, value, description) VALUES ('runtime-test', '1', 'runtime write')").Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Exec("UPDATE system_configs SET value = '2' WHERE key = 'runtime-test'").Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Exec("DELETE FROM system_configs WHERE key = 'runtime-test'").Error; err != nil {
		t.Fatal(err)
	}
	username := fmt.Sprintf("runtime-user-%d", stamp)
	if err := runtime.Exec("INSERT INTO users (username, sign_key) VALUES (?, ?)", username, "runtime-sign-key").Error; err != nil {
		t.Fatal(err)
	}
	var userCount int64
	if err := runtime.Table("users").Where("username = ?", username).Count(&userCount).Error; err != nil || userCount != 1 {
		t.Fatalf("runtime user select count = %d, err = %v", userCount, err)
	}
	if err := runtime.Exec("UPDATE users SET nickname = ? WHERE username = ?", "runtime user", username).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Exec("DELETE FROM users WHERE username = ?", username).Error; err != nil {
		t.Fatal(err)
	}
	var accountID int64
	if err := owner.Raw("INSERT INTO ledger_accounts (forum_user_id) VALUES (2001) RETURNING id").Scan(&accountID).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Exec(`INSERT INTO ledger_entries (account_id, action, available_delta, source_kind, source_id, idempotency_key, occurred_at) VALUES (?, 'earn', 1, 'test', 'runtime', 'runtime-key', now())`, accountID).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Exec("INSERT INTO audit_logs (actor_id, reason) VALUES ('runtime-test', 'allowed')").Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Exec(`INSERT INTO outbox_events (operation_id, target, target_key, payload) VALUES ('runtime-sequence-test', 'test', 'key', '{}'::jsonb)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"UPDATE ledger_entries SET available_delta = 2", "DELETE FROM ledger_entries", "TRUNCATE ledger_entries", "ALTER TABLE ledger_entries DISABLE TRIGGER ledger_entries_immutable", "DROP TRIGGER ledger_entries_immutable ON ledger_entries"} {
		if err := runtime.Exec(statement).Error; err == nil {
			t.Fatalf("runtime mutation succeeded: %s", statement)
		}
	}
	var count int64
	if err := runtime.Table("ledger_entries").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("runtime ledger rows = %d, err = %v", count, err)
	}
}
