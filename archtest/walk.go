package archtest

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const goModFile = "go.mod"

var (
	errNoGoMod      = errors.New("no go.mod above the working directory: set Config.Root")
	errNoModuleLine = errors.New("no module line in go.mod")

	// testPackage matches the directory of a test-support package (vehicletest,
	// gocoretest): fixtures and doubles, where fail-fast panics and wall-clock
	// reads are idiomatic.
	testPackage = regexp.MustCompile(`^[a-z0-9]+test$`)
)

// Source is one parsed Go file of the module. An application's own guards
// walk the same sources with Sources.
type Source struct {
	Rel    string // relative to the module root, forward slashes
	IsTest bool   // a _test.go file or a file of a test-support package (a directory named *test)
	File   *ast.File

	fset *token.FileSet
}

// At is "file:line" of a node, for a message.
func (src Source) At(node ast.Node) string {
	return fmt.Sprintf("%s:%d", src.Rel, src.fset.Position(node.Pos()).Line)
}

func resolveRoot(root string) (string, error) {
	if root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", fmt.Errorf("root %s: %w", root, err)
		}

		return abs, nil
	}

	dir, err := filepath.Abs(".")
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}

	for {
		_, statErr := os.Stat(filepath.Join(dir, goModFile))
		if statErr == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errNoGoMod
		}

		dir = parent
	}
}

// modulePath reads the module line of root/go.mod.
func modulePath(root string) (string, error) {
	data, err := os.OpenRoot(root)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", root, err)
	}

	defer func() { _ = data.Close() }() // read-only handle: a close error changes nothing

	content, err := data.ReadFile(goModFile)
	if err != nil {
		return "", fmt.Errorf("reading %s/%s: %w", root, goModFile, err)
	}

	for line := range strings.SplitSeq(string(content), "\n") {
		rest, found := strings.CutPrefix(strings.TrimSpace(line), "module")
		if found && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			return strings.Trim(strings.TrimSpace(rest), "\"`"), nil
		}
	}

	return "", fmt.Errorf("%w: %s", errNoModuleLine, root)
}

// walk parses every Go file of the module at root, leaving out testdata,
// vendor, node_modules, hidden and underscore folders and nested modules.
func walk(root string) ([]Source, error) {
	fset := token.NewFileSet()

	var files []Source

	err := filepath.WalkDir(root, func(full string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return visitDir(root, full, entry.Name())
		}

		if !strings.HasSuffix(full, ".go") {
			return nil
		}

		src, err := parseSource(fset, root, full)
		if err != nil {
			return err
		}

		files = append(files, src)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}

	return files, nil
}

func visitDir(root, full, name string) error {
	if full == root {
		return nil
	}

	if name == "testdata" || name == "vendor" || name == "node_modules" || name[0] == '.' || name[0] == '_' {
		return filepath.SkipDir
	}

	_, err := os.Stat(filepath.Join(full, goModFile))
	if err == nil {
		return filepath.SkipDir // a module of its own
	}

	return nil
}

func parseSource(fset *token.FileSet, root, full string) (Source, error) {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return Source{}, fmt.Errorf("relative path of %s: %w", full, err)
	}

	rel = filepath.ToSlash(rel)

	file, err := parser.ParseFile(fset, full, nil, parser.SkipObjectResolution)
	if err != nil {
		return Source{}, fmt.Errorf("parsing %s: %w", rel, err)
	}

	isTest := strings.HasSuffix(rel, "_test.go")
	for seg := range strings.SplitSeq(path.Dir(rel), "/") {
		isTest = isTest || testPackage.MatchString(seg)
	}

	return Source{Rel: rel, IsTest: isTest, File: file, fset: fset}, nil
}
