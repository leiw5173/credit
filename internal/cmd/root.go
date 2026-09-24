/*
Copyright 2025 linux.do

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

package cmd

import (
	"context"
	"fmt"
	"log"

	"github.com/linux-do/credit/internal/config"
	"github.com/linux-do/credit/internal/db"
	"github.com/linux-do/credit/internal/db/migrator"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use: "linux-do-credit",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd == migrateCmd {
			return nil
		}
		if !config.Config.Database.Enabled {
			return fmt.Errorf("runtime database must be enabled")
		}
		return migrator.VerifyRuntime(db.DB(context.Background()))
	},
}

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.AddCommand(apiCmd, workerCmd, schedulerCmd, migrateCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("[CMD] execute failed; %s\n", err)
	}
}
