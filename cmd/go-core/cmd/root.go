package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/wssto2/go-core/cmd/go-core/style"
)

var rootCmd = &cobra.Command{
	Use:   "go-core",
	Short: "go-core CLI — scaffold, generate, and manage your go-core project",
	Long:  style.Banner.Render("go-core CLI") + "\n\nScaffold modules, events, migrations and more for go-core projects.",
	// When called with no arguments, show an interactive menu.
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInteractiveMenu()
	},
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(newCmd)
}

type menuAction string

const (
	actionNewModule    menuAction = "new:module"
	actionNewMigration menuAction = "new:migration"
	actionNewEvent     menuAction = "new:event"
	actionQuit         menuAction = "quit"
)

func runInteractiveMenu() error {
	fmt.Println(style.Banner.Render("go-core CLI"))
	fmt.Println()

	var action menuAction

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[menuAction]().
				Title("What would you like to do?").
				Options(
					huh.NewOption("✦  New module      — scaffold a full domain module", actionNewModule),
					huh.NewOption("✦  New migration   — generate a migration stub", actionNewMigration),
					huh.NewOption("✦  New event       — generate a domain event", actionNewEvent),
					huh.NewOption("✕  Quit", actionQuit),
				).
				Value(&action),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	fmt.Println()

	switch action {
	case actionNewModule:
		return runModule(moduleCmd, nil)
	case actionNewMigration:
		return runMigration(migrationCmd, nil)
	case actionNewEvent:
		return runEvent(eventCmd, nil)
	case actionQuit:
		return nil
	}
	return nil
}

