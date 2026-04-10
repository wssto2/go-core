package cmd

import (
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Scaffold new components (module, migration, event, worker)",
}

func init() {
	newCmd.AddCommand(moduleCmd)
	newCmd.AddCommand(migrationCmd)
	newCmd.AddCommand(eventCmd)
}
