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

var eventCmd = &cobra.Command{
	Use:     "event [name]",
	Short:   "Generate a domain event struct",
	Example: "  go-core new event OrderShipped --package order",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runEvent,
}

var (
	eventOutDir string
	eventPkg    string
)

func init() {
	eventCmd.Flags().StringVarP(&eventOutDir, "out", "o", "", "Output directory (default: current directory)")
	eventCmd.Flags().StringVarP(&eventPkg, "package", "p", "", "Package name (default: directory name)")
}

func runEvent(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	goModule := scaffold.DetectGoModule(cwd)

	var name string
	if len(args) > 0 {
		name = args[0]
	} else {
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Event name").
					Description("PascalCase name without 'Event' suffix, e.g. OrderShipped").
					Placeholder("OrderShipped").
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("event name is required")
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
	outDir := eventOutDir
	if outDir == "" {
		outDir = cwd
	}

	pkg := eventPkg
	if pkg == "" {
		pkg = filepath.Base(outDir)
	}

	path, err := scaffold.GenerateEvent(outDir, name, goModule, pkg)
	if err != nil {
		return fmt.Errorf("%s %s", style.Error.Render("✗"), err)
	}

	rel, _ := filepath.Rel(cwd, path)
	fmt.Printf("\n%s Created %s\n\n", style.Success.Render("✓"), style.File.Render(rel))
	return nil
}
