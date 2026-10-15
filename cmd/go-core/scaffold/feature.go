// Package scaffold generates the files of a go-core project: modules, features, migrations and events.
package scaffold

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"io/fs"
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

	errBadName   = errors.New("invalid name")
	errOverwrite = errors.New("refusing to overwrite existing file(s)")
	errNoModule  = errors.New("no module")
)

const (
	dirMode  = 0o750
	fileMode = 0o600
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
			return FeatureData{}, fmt.Errorf("%w: %s %q must match %s: a single lowercase word, no separators",
				errBadName, name, value, wordRe)
		}

		if token.IsKeyword(value) {
			return FeatureData{}, fmt.Errorf("%w: %s %q is a Go keyword, not a package name", errBadName, name, value)
		}
	}

	if entity == "" {
		entity = strings.ToUpper(feature[:1]) + feature[1:]
	}

	if !entityRe.MatchString(entity) {
		return FeatureData{}, fmt.Errorf("%w: entity %q must match %s: a PascalCase type name", errBadName, entity, entityRe)
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
func featureFiles(data FeatureData) []featureFile {
	return []featureFile{
		{"domain_entity.tmpl", filepath.Join("internal", "domain", data.Domain, data.Feature, data.Feature+".go")},
		{"domain_repository.tmpl", filepath.Join("internal", "domain", data.Domain, data.Feature, "repository.go")},
		{"domain_errors.tmpl", filepath.Join("internal", "domain", data.Domain, data.Feature, "errors.go")},
		{"app_service.tmpl", filepath.Join("internal", "application", data.Domain, data.Feature, "service.go")},
		{"app_inputs.tmpl", filepath.Join("internal", "application", data.Domain, data.Feature, "inputs.go")},
		{"app_policies.tmpl", filepath.Join("internal", "application", data.Domain, data.Feature, "policies.go")},
		{"db_repository.tmpl", filepath.Join("internal", "infrastructure", "db", "repository",
			data.Feature+"_repository.go")},
		{"http_routes.tmpl", filepath.Join("internal", "infrastructure", "http", data.Domain, data.Feature, "routes.go")},
		{"http_handler.tmpl", filepath.Join("internal", "infrastructure", "http", data.Domain, data.Feature, "handler.go")},
		{"http_dto.tmpl", filepath.Join("internal", "infrastructure", "http", data.Domain, data.Feature, "dto",
			data.Feature+"_response.go")},
		{"bootstrap_install.tmpl", filepath.Join("internal", "bootstrap", data.Feature, "install.go")},
	}
}

// GenerateFeature writes the layered skeleton of a feature under root and returns the paths written, relative to root.
// It renders and formats every file first and refuses, changing nothing, when any target exists.
func GenerateFeature(root string, data FeatureData) ([]string, error) {
	files := featureFiles(data)

	var existing []string

	for _, file := range files {
		_, err := os.Stat(filepath.Join(root, file.path))
		if err == nil {
			existing = append(existing, file.path)
		}
	}

	if len(existing) > 0 {
		return nil, fmt.Errorf("%w:\n  %s", errOverwrite, strings.Join(existing, "\n  "))
	}

	rendered, err := renderFeature(files, data)
	if err != nil {
		return nil, err
	}

	written := make([]string, 0, len(files))

	for index, file := range files {
		abs := filepath.Join(root, file.path)

		err := os.MkdirAll(filepath.Dir(abs), dirMode)
		if err != nil {
			return written, fmt.Errorf("mkdir %s: %w", filepath.Dir(file.path), err)
		}

		err = os.WriteFile(abs, rendered[index], fileMode)
		if err != nil {
			return written, fmt.Errorf("write %s: %w", file.path, err)
		}

		written = append(written, file.path)
	}

	return written, nil
}

// renderFeature renders and gofmts every file, so a template error leaves the tree untouched.
func renderFeature(files []featureFile, data FeatureData) ([][]byte, error) {
	tmpls, err := template.ParseFS(featureTemplates, "feature/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	rendered := make([][]byte, len(files))

	for index, file := range files {
		var buf bytes.Buffer

		err := tmpls.ExecuteTemplate(&buf, file.template, data)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", file.template, err)
		}

		rendered[index], err = format.Source(buf.Bytes())
		if err != nil {
			return nil, fmt.Errorf("format %s: %w", file.path, err)
		}
	}

	return rendered, nil
}

// FeatureSteps is what the app must still do by hand after GenerateFeature: the generator never edits the composition
// root, so these show up in the diff for a reviewer.
func FeatureSteps(data FeatureData) string {
	return fmt.Sprintf(`Left to do by hand:

1. Install the feature next to the others in the composition root:

       import %[3]s "%[1]s/internal/bootstrap/%[3]s"

       %[3]s.Install(app)

2. Permissions: Install defines %[4]s.%[3]s:view and %[4]s.%[3]s:manage. Add their labels and descriptions to
   the app's translations, and put them on a navigation node if the app has a menu.

Then run go build ./... && go test ./...

Replace the in-memory %[2]s with a GORM implementation over the feature's table.
`, data.Module, data.RepoType, data.Feature, data.Domain)
}

// FindModule walks up from dir to the nearest go.mod and returns its directory and module path.
func FindModule(dir string) (string, string, error) {
	for current := dir; ; current = filepath.Dir(current) {
		raw, err := fs.ReadFile(os.DirFS(current), "go.mod")
		if err == nil {
			for line := range strings.SplitSeq(string(raw), "\n") {
				path, found := strings.CutPrefix(strings.TrimSpace(line), "module ")
				if found {
					path, _, _ = strings.Cut(path, "//") // a trailing comment

					return current, strings.Trim(strings.TrimSpace(path), `"`), nil
				}
			}

			return "", "", fmt.Errorf("%w: %s has no module line", errNoModule, filepath.Join(current, "go.mod"))
		}

		if current == filepath.Dir(current) {
			return "", "", fmt.Errorf("%w: go.mod not found from %s upward", errNoModule, dir)
		}
	}
}
