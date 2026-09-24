/*
Copyright 2026 linux.do

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package migrator

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

//go:embed sql/*.up.sql
var migrationFiles embed.FS

const migrationAdvisoryLock int64 = 417264938112547

// ValidateVersions rejects duplicate, malformed, and non-contiguous migration versions.
func ValidateVersions(names []string) error {
	seen := make(map[int]bool, len(names))
	for _, name := range names {
		parts := strings.SplitN(filepath.Base(name), "_", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid migration name %q", name)
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil || version < 1 || seen[version] {
			return fmt.Errorf("invalid or duplicate migration version %q", name)
		}
		seen[version] = true
	}
	for version := 1; version <= len(names); version++ {
		if !seen[version] {
			return fmt.Errorf("missing migration version %04d", version)
		}
	}
	return nil
}

// ApplyVersioned applies each embedded PostgreSQL migration once, in order.
func ApplyVersioned(db *gorm.DB) error {
	names, err := fs.Glob(migrationFiles, "sql/*.up.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	if err := ValidateVersions(names); err != nil {
		return err
	}
	sort.Strings(names)

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", migrationAdvisoryLock).Error; err != nil {
			return fmt.Errorf("acquire migration lock: %w", err)
		}
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
			version bigint PRIMARY KEY,
			name text NOT NULL UNIQUE,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`).Error; err != nil {
			return fmt.Errorf("create schema_migrations: %w", err)
		}

		for _, name := range names {
			version, err := migrationVersion(name)
			if err != nil {
				return err
			}
			var appliedName string
			result := tx.Raw("SELECT name FROM schema_migrations WHERE version = ?", version).Scan(&appliedName)
			if result.Error != nil {
				return fmt.Errorf("check migration %04d: %w", version, result.Error)
			}
			if result.RowsAffected > 0 {
				if appliedName != filepath.Base(name) {
					return fmt.Errorf("migration version %04d already recorded as %q", version, appliedName)
				}
				continue
			}

			sql, err := migrationFiles.ReadFile(name)
			if err != nil {
				return fmt.Errorf("read migration %q: %w", name, err)
			}
			if err := tx.Exec(string(sql)).Error; err != nil {
				return fmt.Errorf("apply migration %q: %w", name, err)
			}
			if err := tx.Exec("INSERT INTO schema_migrations (version, name) VALUES (?, ?)", version, filepath.Base(name)).Error; err != nil {
				return fmt.Errorf("record migration %q: %w", name, err)
			}
		}
		return nil
	})
}

func migrationVersion(name string) (int, error) {
	parts := strings.SplitN(filepath.Base(name), "_", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid migration name %q", name)
	}
	version, err := strconv.Atoi(parts[0])
	if err != nil || version < 1 {
		return 0, fmt.Errorf("invalid migration version %q", name)
	}
	return version, nil
}
