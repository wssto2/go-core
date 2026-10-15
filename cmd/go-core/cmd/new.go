package cmd

import (
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Scaffold new components (module, feature, migration, event)",
}

func init() {
	newCmd.AddCommand(moduleCmd)
	newCmd.AddCommand(featureCmd)
	newCmd.AddCommand(migrationCmd)
	newCmd.AddCommand(eventCmd)
}
