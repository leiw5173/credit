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

// GrantRuntimePrivileges grants the non-owner runtime the explicit legacy and ledger capabilities it needs.
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
	legacyTables := "users, user_pay_configs, merchant_api_keys, merchant_payment_links, orders, order_transfers, system_configs, disputes, red_envelopes, red_envelope_claims, uploads"
	statements := []string{
		"REVOKE ALL ON ledger_entries, ledger_accounts, outbox_events, audit_logs FROM " + quoted,
		"REVOKE ALL ON ALL SEQUENCES IN SCHEMA " + quotedSchema + " FROM " + quoted,
		"GRANT USAGE ON SCHEMA " + quotedSchema + " TO " + quoted,
		"GRANT SELECT, INSERT ON ledger_entries TO " + quoted,
		"GRANT USAGE, SELECT ON SEQUENCE ledger_entries_id_seq TO " + quoted,
		"GRANT SELECT ON schema_migrations, ledger_accounts TO " + quoted,
		"GRANT SELECT, INSERT ON outbox_events, audit_logs TO " + quoted,
		"GRANT USAGE, SELECT ON SEQUENCE outbox_events_id_seq, audit_logs_id_seq TO " + quoted,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON " + legacyTables + " TO " + quoted,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA " + quotedSchema + " TO " + quoted,
	}
	for _, statement := range statements {
		if err := owner.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

// VerifyRuntime fails closed unless the runtime login sees all required guards and least privileges.
func VerifyRuntime(runtime *gorm.DB) error {
	var required, total int64
	if err := runtime.Table("schema_migrations").Where("version = ? AND name = ?", 1, "0001_credis_ledger.up.sql").Count(&required).Error; err != nil {
		return fmt.Errorf("schema migrations unavailable: %w", err)
	}
	if err := runtime.Table("schema_migrations").Count(&total).Error; err != nil {
		return fmt.Errorf("schema migrations unavailable: %w", err)
	}
	if required != 1 || total != 1 {
		return fmt.Errorf("required schema migration set is not current")
	}
	var objects int64
	if err := runtime.Raw("SELECT count(*) FROM unnest(ARRAY['ledger_accounts','ledger_entries','audit_logs','outbox_events']::text[]) n WHERE to_regclass(n) IS NOT NULL").Scan(&objects).Error; err != nil || objects != 4 {
		return fmt.Errorf("required ledger objects unavailable")
	}
	var guards int64
	if err := runtime.Raw("SELECT count(*) FROM pg_trigger WHERE tgrelid = 'ledger_entries'::regclass AND tgenabled <> 'D' AND tgname IN ('ledger_entries_immutable','ledger_entries_no_truncate')").Scan(&guards).Error; err != nil || guards != 2 {
		return fmt.Errorf("ledger immutability guards unavailable")
	}
	var superuser, inherit, member bool
	if err := runtime.Raw("SELECT r.rolsuper, r.rolinherit, pg_has_role(current_user, c.relowner, 'MEMBER') FROM pg_roles r CROSS JOIN pg_class c WHERE r.rolname = current_user AND c.oid = 'ledger_entries'::regclass").Row().Scan(&superuser, &inherit, &member); err != nil {
		return fmt.Errorf("check runtime role: %w", err)
	}
	if superuser || inherit || member {
		return fmt.Errorf("runtime role can escalate to ledger owner")
	}
	var selectOK, insertOK, updateOK, deleteOK, truncateOK bool
	if err := runtime.Raw("SELECT has_table_privilege(current_user, 'ledger_entries', 'SELECT'), has_table_privilege(current_user, 'ledger_entries', 'INSERT'), has_table_privilege(current_user, 'ledger_entries', 'UPDATE'), has_table_privilege(current_user, 'ledger_entries', 'DELETE'), has_table_privilege(current_user, 'ledger_entries', 'TRUNCATE')").Row().Scan(&selectOK, &insertOK, &updateOK, &deleteOK, &truncateOK); err != nil {
		return fmt.Errorf("check ledger privileges: %w", err)
	}
	if !selectOK || !insertOK || updateOK || deleteOK || truncateOK {
		return fmt.Errorf("runtime ledger privileges are not least privilege")
	}
	return nil
}
