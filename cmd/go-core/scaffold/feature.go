package scaffold

import (
	"bufio"
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

//go:embed feature/*.tmpl
var featureTemplates embed.FS

var (
	wordRe   = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	entityRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
)

// FeatureData is what every file of a feature is rendered from.
type FeatureData struct {
	Module      string // the target's module path, from its go.mod
	Domain      string // a single lowercase word
	Feature     string // a single lowercase word
	Entity      string // PascalCase
	Recv        string
	DomainAlias string
	AppAlias    string
	HTTPAlias   string
	RepoType    string
	Namespace   string // the permission namespace, domain.feature
	InputType   string
}

// NewFeatureData validates the names and derives the rest. domain and feature must each be a single lowercase word, the
// convention of routes, permissions and packages; entity defaults to the feature name capitalised.
func NewFeatureData(module, domain, feature, entity string) (FeatureData, error) {
	domain, feature, entity = strings.TrimSpace(domain), strings.TrimSpace(feature), strings.TrimSpace(entity)

	for name, value := range map[string]string{"domain": domain, "feature": feature} {
		if !wordRe.MatchString(value) {
			return FeatureData{}, fmt.Errorf("%s %q must match %s: a single lowercase word, no separators", name, value, wordRe)
		}

		if token.IsKeyword(value) {
			return FeatureData{}, fmt.Errorf("%s %q is a Go keyword and cannot be a package name", name, value)
		}
	}

	if entity == "" {
		entity = strings.ToUpper(feature[:1]) + feature[1:]
	}

	if !entityRe.MatchString(entity) {
		return FeatureData{}, fmt.Errorf("entity %q must match %s: a PascalCase type name", entity, entityRe)
	}

	return FeatureData{
		Module:      module,
		Domain:      domain,
		Feature:     feature,
		Entity:      entity,
		Recv:        "record",
		DomainAlias: "domain" + feature,
		AppAlias:    "app" + feature,
		HTTPAlias:   "http" + feature,
		RepoType:    entity + "Repository",
		Namespace:   domain + "." + feature,
		InputType:   "Create" + entity + "Input",
	}, nil
}

type featureFile struct {
	template string
	path     string
}

// featureFiles is the layered layout of a feature, relative to the module root.
func featureFiles(d FeatureData) []featureFile {
	return []featureFile{
		{"domain_entity.tmpl", filepath.Join("internal", "domain", d.Domain, d.Feature, d.Feature+".go")},
		{"domain_repository.tmpl", filepath.Join("internal", "domain", d.Domain, d.Feature, "repository.go")},
		{"domain_errors.tmpl", filepath.Join("internal", "domain", d.Domain, d.Feature, "errors.go")},
		{"app_service.tmpl", filepath.Join("internal", "application", d.Domain, d.Feature, "service.go")},
		{"app_inputs.tmpl", filepath.Join("internal", "application", d.Domain, d.Feature, "inputs.go")},
		{"app_policies.tmpl", filepath.Join("internal", "application", d.Domain, d.Feature, "policies.go")},
		{"db_repository.tmpl", filepath.Join("internal", "infrastructure", "db", "repository", d.Feature+"_repository.go")},
		{"http_routes.tmpl", filepath.Join("internal", "infrastructure", "http", d.Domain, d.Feature, "routes.go")},
		{"http_handler.tmpl", filepath.Join("internal", "infrastructure", "http", d.Domain, d.Feature, "handler.go")},
		{"http_dto.tmpl", filepath.Join("internal", "infrastructure", "http", d.Domain, d.Feature, "dto", d.Feature+"_response.go")},
		{"bootstrap_install.tmpl", filepath.Join("internal", "bootstrap", d.Feature, "install.go")},
	}
}

// GenerateFeature writes the layered skeleton of a feature under root and returns the paths written, relative to root.
// It renders and formats every file first and refuses, changing nothing, when any target exists.
func GenerateFeature(root string, data FeatureData) ([]string, error) {
	files := featureFiles(data)

	var existing []string

	for _, f := range files {
		if _, err := os.Stat(filepath.Join(root, f.path)); err == nil {
			existing = append(existing, f.path)
		}
	}

	if len(existing) > 0 {
		return nil, fmt.Errorf("refusing to overwrite existing file(s):\n  %s", strings.Join(existing, "\n  "))
	}

	tmpls, err := template.ParseFS(featureTemplates, "feature/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	rendered := make([][]byte, len(files))

	for i, f := range files {
		var buf bytes.Buffer
		if err := tmpls.ExecuteTemplate(&buf, f.template, data); err != nil {
			return nil, fmt.Errorf("render %s: %w", f.template, err)
		}

		if rendered[i], err = format.Source(buf.Bytes()); err != nil {
			return nil, fmt.Errorf("format %s: %w", f.path, err)
		}
	}

	written := make([]string, 0, len(files))

	for i, f := range files {
		abs := filepath.Join(root, f.path)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return written, fmt.Errorf("mkdir %s: %w", filepath.Dir(f.path), err)
		}

		if err := os.WriteFile(abs, rendered[i], 0o644); err != nil {
			return written, fmt.Errorf("write %s: %w", f.path, err)
		}

		written = append(written, f.path)
	}

	return written, nil
}

// FeatureSteps is what the app must still do by hand after GenerateFeature: the generator never edits the composition
// root, so these show up in the diff for a reviewer.
func FeatureSteps(d FeatureData) string {
	return fmt.Sprintf(`Left to do by hand:

1. Install the feature next to the others in the composition root:

       import %[3]s "%[1]s/internal/bootstrap/%[3]s"

       %[3]s.Install(app)

2. Permissions: Install defines %[4]s.%[3]s:view and %[4]s.%[3]s:manage. Add their labels and descriptions to
   the app's translations, and put them on a navigation node if the app has a menu.

Then run go build ./... && go test ./...

Replace the in-memory %[2]s with a GORM implementation over the feature's table.
`, d.Module, d.RepoType, d.Feature, d.Domain)
}

// FindModule walks up from dir to the nearest go.mod and returns its directory and module path.
func FindModule(dir string) (root, module string, err error) {
	for d := dir; ; d = filepath.Dir(d) {
		f, openErr := os.Open(filepath.Join(d, "go.mod"))
		if openErr == nil {
			defer f.Close()

			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				if path, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), "module "); ok {
					path, _, _ = strings.Cut(path, "//") // a trailing comment

					return d, strings.Trim(strings.TrimSpace(path), `"`), nil
				}
			}

			return "", "", fmt.Errorf("%s has no module line", filepath.Join(d, "go.mod"))
		}

		if d == filepath.Dir(d) {
			return "", "", errors.New("go.mod not found from " + dir + " upward")
		}
	}
}
