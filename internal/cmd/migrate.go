package cmd

import (
	"fmt"
	"os"

	"github.com/linux-do/credit/internal/db/migrator"
	"github.com/spf13/cobra"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "apply database migrations with the migration-owner connection",
	RunE: func(cmd *cobra.Command, args []string) error {
		dsn := os.Getenv("CREDIS_MIGRATION_DATABASE_URL")
		role := os.Getenv("CREDIS_RUNTIME_ROLE")
		if dsn == "" || role == "" {
			return fmt.Errorf("migration command requires CREDIS_MIGRATION_DATABASE_URL and CREDIS_RUNTIME_ROLE")
		}
		owner, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			return fmt.Errorf("open migration database: %w", err)
		}
		if err := migrator.Migrate(owner, role); err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		return nil
	},
}
