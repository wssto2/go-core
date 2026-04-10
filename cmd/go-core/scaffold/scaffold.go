package scaffold

import (
	"bufio"
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
)

// ModuleData is passed to every module template.
type ModuleData struct {
	GoModule   string // e.g. "go-core-example"
	Package    string // e.g. "product"
	Pascal     string // e.g. "Product"
	Features   Features
}

// Features controls which optional sections are rendered.
type Features struct {
	Events      bool
	Audit       bool
	Worker      bool
}

// GenerateModule writes all module files into outDir.
func GenerateModule(outDir string, data ModuleData) ([]string, error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	files := moduleFiles(data)
	written := make([]string, 0, len(files))

	for name, tplStr := range files {
		path := filepath.Join(outDir, name)
		if _, err := os.Stat(path); err == nil {
			return written, fmt.Errorf("file already exists: %s", path)
		}

		content, err := render(name, tplStr, data)
		if err != nil {
			return written, fmt.Errorf("render %s: %w", name, err)
		}

		// gofmt the output
		formatted, fmtErr := format.Source(content)
		if fmtErr != nil {
			// write raw so the user can see the problem
			formatted = content
		}

		if err := os.WriteFile(path, formatted, 0644); err != nil {
			return written, fmt.Errorf("write %s: %w", name, err)
		}
		written = append(written, path)
	}

	return written, nil
}

// GenerateMigration creates a timestamped GORM AutoMigrate stub.
func GenerateMigration(outDir, name string, goModule string) (string, error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	data := struct {
		GoModule string
		Name     string
		Pascal   string
	}{GoModule: goModule, Name: name, Pascal: ToPascal(name)}

	content, err := render("migration.go", migrationTpl, data)
	if err != nil {
		return "", err
	}

	formatted, fmtErr := format.Source(content)
	if fmtErr != nil {
		formatted = content
	}

	filename := fmt.Sprintf("migrate_%s.go", ToSnake(name))
	path := filepath.Join(outDir, filename)
	if err := os.WriteFile(path, formatted, 0644); err != nil {
		return "", err
	}
	return path, nil
}

// GenerateEvent creates an event struct file.
func GenerateEvent(outDir, name, goModule, pkg string) (string, error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	data := struct {
		GoModule string
		Package  string
		Pascal   string
		Name     string
	}{GoModule: goModule, Package: pkg, Pascal: ToPascal(name), Name: name}

	content, err := render("event.go", eventTpl, data)
	if err != nil {
		return "", err
	}

	formatted, fmtErr := format.Source(content)
	if fmtErr != nil {
		formatted = content
	}

	path := filepath.Join(outDir, fmt.Sprintf("event_%s.go", ToSnake(name)))
	if err := os.WriteFile(path, formatted, 0644); err != nil {
		return "", err
	}
	return path, nil
}

func render(name, tplStr string, data any) ([]byte, error) {
	funcs := template.FuncMap{
		"lower":      strings.ToLower,
		"upper":      strings.ToUpper,
		"pascal":     ToPascal,
		"snake":      ToSnake,
		"camel":      ToCamel,
		"actionName": ActionName,
	}
	t, err := template.New(name).Funcs(funcs).Parse(tplStr)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// moduleFiles returns the map of filename → template string for a module.
func moduleFiles(data ModuleData) map[string]string {
	files := map[string]string{
		"model.go":      modelTpl,
		"repository.go": repositoryTpl,
		"service.go":    serviceTpl,
		"handler.go":    handlerTpl,
		"requests.go":   requestsTpl,
		"module.go":     moduleTpl,
	}
	if data.Features.Events {
		files["events.go"] = eventsTpl
	}
	if data.Features.Worker {
		files["worker.go"] = workerTpl
	}
	return files
}

// DetectGoModule reads the nearest go.mod and returns the module path.
func DetectGoModule(dir string) string {
	for d := dir; d != filepath.Dir(d); d = filepath.Dir(d) {
		path := filepath.Join(d, "go.mod")
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "module ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "module "))
			}
		}
	}
	return "your-module"
}

// ActionName strips the package prefix from an event Pascal name and returns
// the snake_case action part.
// e.g. Package="order", Pascal="OrderShipped" → "shipped"
//      Package="order", Pascal="Shipped"       → "shipped"
func ActionName(pkg, pascal string) string {
	pkgPascal := ToPascal(pkg)
	trimmed := pascal
	if strings.HasPrefix(strings.ToLower(pascal), strings.ToLower(pkgPascal)) {
		trimmed = pascal[len(pkgPascal):]
	}
	if trimmed == "" {
		trimmed = pascal
	}
	return ToSnake(trimmed)
}
func ToPascal(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	var b strings.Builder
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		runes := []rune(p)
		b.WriteRune(unicode.ToUpper(runes[0]))
		b.WriteString(string(runes[1:]))
	}
	return b.String()
}

// ToCamel converts "ProductImage" or "product_image" → "productImage".
func ToCamel(s string) string {
	p := ToPascal(s)
	if p == "" {
		return ""
	}
	runes := []rune(p)
	return string(unicode.ToLower(runes[0])) + string(runes[1:])
}

// ToSnake converts "ProductImage" or "product-image" → "product_image".
func ToSnake(s string) string {
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteRune('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
