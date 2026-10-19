package cmd

import (
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Scaffold new components (feature, migration, event)",
}

func init() {
	newCmd.AddCommand(newFeatureCmd())
	newCmd.AddCommand(migrationCmd)
	newCmd.AddCommand(eventCmd)
}
