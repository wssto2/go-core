package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/wssto2/go-core/cmd/go-core/scaffold"
	"github.com/wssto2/go-core/cmd/go-core/style"
)

var migrationCmd = &cobra.Command{
	Use:     "migration [name]",
	Short:   "Generate a migration stub",
	Example: "  go-core new migration add_status_to_orders",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runMigration,
}

var migrationOutDir string

func init() {
	migrationCmd.Flags().StringVarP(&migrationOutDir, "out", "o", "", "Output directory (default: ./internal/migrations)")
}

func runMigration(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	goModule := scaffold.DetectGoModule(cwd)

	var name string
	if len(args) > 0 {
		name = args[0]
	} else {
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Migration name").
					Description("Descriptive snake_case name, e.g. add_status_to_orders").
					Placeholder("add_status_to_orders").
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("migration name is required")
						}
						return nil
					}).
					Value(&name),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}
	}

	name = strings.TrimSpace(name)
	outDir := migrationOutDir
	if outDir == "" {
		outDir = filepath.Join(cwd, "internal", "migrations")
	}

	path, err := scaffold.GenerateMigration(outDir, name, goModule)
	if err != nil {
		return fmt.Errorf("%s %s", style.Error.Render("✗"), err)
	}

	rel, _ := filepath.Rel(cwd, path)
	fmt.Printf("\n%s Created %s\n\n", style.Success.Render("✓"), style.File.Render(rel))
	return nil
}
