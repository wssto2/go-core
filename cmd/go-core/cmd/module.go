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

var moduleCmd = &cobra.Command{
	Use:     "module [name]",
	Short:   "Scaffold a new go-core domain module",
	Example: "  go-core new module product\n  go-core new module order --out ./internal/domain/order",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runModule,
}

var moduleOutDir string

func init() {
	moduleCmd.Flags().StringVarP(&moduleOutDir, "out", "o", "", "Output directory (default: ./internal/domain/<name>)")
}

func runModule(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	goModule := scaffold.DetectGoModule(cwd)

	data := scaffold.ModuleData{
		GoModule: goModule,
		Features: scaffold.Features{
			Audit:  true,
			Events: false,
			Worker: false,
		},
	}

	// --- Interactive form when no name argument is given ---
	if len(args) == 0 {
		if err := runModuleForm(&data, cwd); err != nil {
			return err
		}
	} else {
		name := strings.ToLower(args[0])
		data.Package = name
		data.Pascal = scaffold.ToPascal(name)
		if moduleOutDir == "" {
			moduleOutDir = filepath.Join(cwd, "internal", "domain", name)
		}
	}

	// Always run a confirmation form for features if they weren't set via the
	// interactive wizard (i.e., when name was passed as an arg).
	if len(args) > 0 {
		if err := runFeaturesForm(&data); err != nil {
			return err
		}
	}

	// Generate
	written, err := scaffold.GenerateModule(moduleOutDir, data)
	if err != nil {
		return fmt.Errorf("%s %s", style.Error.Render("✗"), err)
	}

	fmt.Println()
	fmt.Printf("%s Module %s scaffolded successfully\n\n",
		style.Success.Render("✓"),
		style.Bold.Render(data.Pascal),
	)
	for _, f := range written {
		rel, _ := filepath.Rel(cwd, f)
		fmt.Printf("  %s %s\n", style.Muted.Render("→"), style.File.Render(rel))
	}
	fmt.Println()
	fmt.Printf("%s Register your module in main.go:\n", style.Muted.Render("💡"))
	fmt.Printf("   %s\n", style.Bold.Render(fmt.Sprintf(".WithModules(%s.NewModule())", data.Package)))

	return nil
}

func runModuleForm(data *scaffold.ModuleData, cwd string) error {
	var name string
	var outDir string
	var features []string

	defaultOut := func() string {
		if name == "" {
			return filepath.Join(cwd, "internal", "domain", "<name>")
		}
		return filepath.Join(cwd, "internal", "domain", name)
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Module name").
				Description("Lowercase, e.g. product, order, invoice").
				Placeholder("product").
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("module name is required")
					}
					return nil
				}).
				Value(&name),

			huh.NewInput().
				Title("Output directory").
				Description("Where to create the module files").
				Value(&outDir).
				Placeholder(defaultOut()),
		),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Features").
				Description("Select optional features to include").
				Options(
					huh.NewOption("Events (domain events + outbox)", "events"),
					huh.NewOption("Audit log (record create/update/delete actors)", "audit"),
					huh.NewOption("Background worker stub", "worker"),
				).
				Value(&features),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	name = strings.ToLower(strings.TrimSpace(name))
	data.Package = name
	data.Pascal = scaffold.ToPascal(name)

	if strings.TrimSpace(outDir) == "" || outDir == defaultOut() {
		outDir = filepath.Join(cwd, "internal", "domain", name)
	}
	moduleOutDir = outDir

	for _, f := range features {
		switch f {
		case "events":
			data.Features.Events = true
		case "audit":
			data.Features.Audit = true
		case "worker":
			data.Features.Worker = true
		}
	}
	return nil
}

func runFeaturesForm(data *scaffold.ModuleData) error {
	var features []string
	defaultSelected := []string{}
	if data.Features.Audit {
		defaultSelected = append(defaultSelected, "audit")
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Features").
				Description("Select optional features to include").
				Options(
					huh.NewOption("Events (domain events + outbox)", "events"),
					huh.NewOption("Audit log (record create/update/delete actors)", "audit"),
					huh.NewOption("Background worker stub", "worker"),
				).
				Value(&features),
		),
	)

	// Pre-select defaults
	_ = defaultSelected // huh v1 does not support pre-selection via fluent API for MultiSelect

	if err := form.Run(); err != nil {
		return err
	}

	// Reset then apply selections
	data.Features = scaffold.Features{}
	for _, f := range features {
		switch f {
		case "events":
			data.Features.Events = true
		case "audit":
			data.Features.Audit = true
		case "worker":
			data.Features.Worker = true
		}
	}
	return nil
}
