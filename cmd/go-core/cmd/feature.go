package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wssto2/go-core/cmd/go-core/scaffold"
	"github.com/wssto2/go-core/cmd/go-core/style"
)

var featureCmd = &cobra.Command{
	Use:   "feature",
	Short: "Scaffold the layered skeleton of a feature (domain, application, db, http, install)",
	Long: `Writes 11 files under internal/ in the module of the current directory: a domain entity with its repository port,
an application service with inputs and permissions, an in-memory repository, typed routes with their handlers and DTO,
and an Install that builds the service and mounts the routes. It never overwrites a file and never edits the
composition root.`,
	Example: "  go-core new feature --domain crm --feature notes\n  go-core new feature --domain crm --feature notes --entity Note",
	Args:    cobra.NoArgs,
	RunE:    runFeature,
}

var featureDomain, featureName, featureEntity string

func init() {
	featureCmd.Flags().StringVar(&featureDomain, "domain", "", "domain segment, a single lowercase word (crm)")
	featureCmd.Flags().StringVar(&featureName, "feature", "", "feature segment, a single lowercase word (notes)")
	featureCmd.Flags().StringVar(&featureEntity, "entity", "", "entity type name in PascalCase (default: the feature name capitalised)")
	_ = featureCmd.MarkFlagRequired("domain")  // cobra only errors for a flag that does not exist
	_ = featureCmd.MarkFlagRequired("feature") // cobra only errors for a flag that does not exist
}

func runFeature(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	root, module, err := scaffold.FindModule(cwd)
	if err != nil {
		return err
	}

	data, err := scaffold.NewFeatureData(module, featureDomain, featureName, featureEntity)
	if err != nil {
		return err
	}

	written, err := scaffold.GenerateFeature(root, data)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\n%s Scaffolded feature %s/%s in %s: %d files\n", style.Success.Render("✓"), data.Domain, data.Feature, filepath.Base(root), len(written))

	for _, path := range written {
		fmt.Fprintln(out, "  "+style.File.Render(path))
	}

	fmt.Fprintln(out)
	fmt.Fprint(out, scaffold.FeatureSteps(data))

	return nil
}
