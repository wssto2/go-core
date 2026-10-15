package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wssto2/go-core/cmd/go-core/scaffold"
	"github.com/wssto2/go-core/cmd/go-core/style"
)

const featureLong = `Writes 11 files under internal/ in the module of the current directory: a domain entity with its
repository port, an application service with inputs and permissions, an in-memory repository, typed routes with their
handlers and DTO, and an Install that builds the service and mounts the routes. It never overwrites a file and never
edits the composition root.`

func newFeatureCmd() *cobra.Command {
	var domain, feature, entity string

	cmd := &cobra.Command{
		Use:   "feature",
		Short: "Scaffold the layered skeleton of a feature (domain, application, db, http, install)",
		Long:  featureLong,
		Example: "  go-core new feature --domain crm --feature notes\n" +
			"  go-core new feature --domain crm --feature notes --entity Note",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFeature(cmd, domain, feature, entity)
		},
	}

	cmd.Flags().StringVar(&domain, "domain", "", "domain segment, a single lowercase word (crm)")
	cmd.Flags().StringVar(&feature, "feature", "", "feature segment, a single lowercase word (notes)")
	cmd.Flags().StringVar(&entity, "entity", "", "entity type name in PascalCase (default: the feature name capitalised)")

	// MarkFlagRequired errors only for a flag that does not exist.
	_ = cmd.MarkFlagRequired("domain")
	_ = cmd.MarkFlagRequired("feature")

	return cmd
}

func runFeature(cmd *cobra.Command, domain, feature, entity string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}

	root, module, err := scaffold.FindModule(cwd)
	if err != nil {
		return fmt.Errorf("find the module: %w", err)
	}

	data, err := scaffold.NewFeatureData(module, domain, feature, entity)
	if err != nil {
		return fmt.Errorf("feature: %w", err)
	}

	written, err := scaffold.GenerateFeature(root, data)
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	var out strings.Builder

	fmt.Fprintf(&out, "\n%s Scaffolded feature %s/%s in %s: %d files\n",
		style.Success.Render("✓"), data.Domain, data.Feature, filepath.Base(root), len(written))

	for _, path := range written {
		out.WriteString("  " + style.File.Render(path) + "\n")
	}

	out.WriteString("\n" + scaffold.FeatureSteps(data))

	_, err = io.WriteString(cmd.OutOrStdout(), out.String())
	if err != nil {
		return fmt.Errorf("write the summary: %w", err)
	}

	return nil
}
