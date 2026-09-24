package migrator

import (
	"fmt"
	"regexp"

	"gorm.io/gorm"
)

var roleNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

func quotedRole(role string) (string, error) {
	if !roleNamePattern.MatchString(role) {
		return "", fmt.Errorf("invalid runtime role identifier")
	}
	return `"` + role + `"`, nil
}

// GrantRuntimePrivileges grants the non-owner runtime only read/append access.
func GrantRuntimePrivileges(owner *gorm.DB, role string) error {
	quoted, err := quotedRole(role)
	if err != nil {
		return err
	}
	var schema string
	if err := owner.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		return fmt.Errorf("read current schema: %w", err)
	}
	quotedSchema, err := quotedRole(schema)
	if err != nil {
		return fmt.Errorf("invalid schema identifier: %w", err)
	}
	statements := []string{
		"REVOKE ALL ON ledger_entries, ledger_accounts, outbox_events, audit_logs FROM " + quoted,
		"REVOKE ALL ON ALL SEQUENCES IN SCHEMA " + quotedSchema + " FROM " + quoted,
		"GRANT USAGE ON SCHEMA " + quotedSchema + " TO " + quoted,
		"GRANT SELECT, INSERT ON ledger_entries TO " + quoted,
		"GRANT USAGE, SELECT ON SEQUENCE ledger_entries_id_seq TO " + quoted,
		"GRANT SELECT ON schema_migrations, ledger_accounts TO " + quoted,
		"GRANT SELECT, INSERT ON outbox_events, audit_logs TO " + quoted,
		"GRANT USAGE, SELECT ON SEQUENCE outbox_events_id_seq, audit_logs_id_seq TO " + quoted,
	}
	for _, statement := range statements {
		if err := owner.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

// VerifyRuntime fails closed unless the runtime login sees the required schema and least privileges.
func VerifyRuntime(runtime *gorm.DB) error {
	var count int64
	if err := runtime.Table("schema_migrations").Where("version = ?", 1).Count(&count).Error; err != nil || count != 1 {
		if err != nil {
			return fmt.Errorf("schema migrations unavailable: %w", err)
		}
		return fmt.Errorf("required schema migration 0001 is not current")
	}
	var allowed, forbidden bool
	if err := runtime.Raw("SELECT has_table_privilege(current_user, 'ledger_entries', 'SELECT, INSERT'), has_table_privilege(current_user, 'ledger_entries', 'UPDATE, DELETE, TRUNCATE')").Row().Scan(&allowed, &forbidden); err != nil {
		return fmt.Errorf("check ledger privileges: %w", err)
	}
	if !allowed || forbidden {
		return fmt.Errorf("runtime ledger privileges are not least privilege")
	}
	return nil
}
