// Package archtest holds an application's architecture to its own rules, from
// its source: a test calls Run with the layout of the module and every rule
// that is not switched off is checked with go/parser, no type information and
// no dependency beyond the standard library and go-core's validation.
//
// The rules are the house rules of a layered go-core application (domain,
// application, infrastructure, interfaces):
//
//   - NoPanic, NoContextBackground, NoSleep, NoDatabaseSQL, ErrorfWrap and
//     HandlersNoPersistence: call and import conventions;
//   - LayerImports, DomainPurity, ApplicationPurity, NoTimeNowDomain,
//     NoTimeNowApplication and CrossAggregate: the direction of dependencies;
//   - DomainContextFirst, ServiceContextFirst: context.Context is the first
//     parameter of every domain port and every exported application service method;
//   - BoundedStrings: every string of an input struct has a validation max: bound;
//   - RoutePaths, RouteIdentifiers, ValidationRules: the names and validation
//     rules a route declares;
//   - RouteAccess: every route.Get/Post/Put/Patch/Delete/Raw chain names who may
//     call it (Requires, RequiresAny, RequiresAll, Public, PersonOnly or
//     Authenticated);
//   - ServiceRepositories: a service injects only its own aggregate's repositories;
//   - RepositoryShape: the transaction and tenant-scope plumbing of go-core is
//     reached from one file of the repository package only.
//
// A rule whose layer is not configured is not run. Banned adds the rules of the
// application itself (a removed API, a package one layer must not import).
//
// Pre-existing violations are frozen in a ratchet: Config.Allowed lists, per
// rule, the files (for ServiceRepositories "file|domain/path") that may still
// break it. A new violation fails the test, and so does a stale entry, so the
// list only ever shrinks.
//
//	func TestArchitecture(t *testing.T) {
//		archtest.Run(t, archtest.Config{
//			Layers: archtest.Layers{
//				Domain:         "internal/domain",
//				Application:    "internal/application",
//				Infrastructure: "internal/infrastructure",
//				Handlers:       "internal/infrastructure/http",
//				Persistence:    "internal/infrastructure/db",
//			},
//			Allowed: map[string][]string{
//				archtest.NoTimeNowDomain: {"internal/domain/shared/clock.go"},
//			},
//			SharedDomains: []string{"locale", "settings"},
//			Banned: []archtest.Banned{{
//				Import: "example.com/legacy/gate",
//				Why:    "routes are gated by Requires; the gate is gone",
//			}},
//		})
//	}
package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The rule names, for Config.Allowed and Config.Skip.
const (
	NoPanic               = "no-panic"
	NoContextBackground   = "no-context-background"
	NoSleep               = "no-sleep"
	NoDatabaseSQL         = "no-database-sql"
	ErrorfWrap            = "errorf-wrap"
	HandlersNoPersistence = "handlers-no-persistence"
	LayerImports          = "layer-imports"
	DomainPurity          = "domain-purity"
	ApplicationPurity     = "application-purity"
	NoTimeNowDomain       = "no-time-now-domain"
	NoTimeNowApplication  = "no-time-now-application"
	CrossAggregate        = "cross-aggregate"
	DomainContextFirst    = "domain-context-first"
	ServiceContextFirst   = "service-context-first"
	BoundedStrings        = "bounded-strings"
	RoutePaths            = "route-paths"
	RouteIdentifiers      = "route-identifiers"
	ValidationRules       = "validation-rules"
	RouteAccess           = "route-access"
	ServiceRepositories   = "service-repositories"
	RepositoryShape       = "repository-shape"
)

// Layers are the directories of the module's layers, relative to its root. A
// layer that is empty is absent: the rules that need it are not run.
type Layers struct {
	Domain         string // entities and the ports (repository interfaces); framework-free
	Application    string // services, inputs, policies; depends on the domain only
	Infrastructure string // adapters: the database, HTTP, integrations
	Interfaces     string // delivery, when it is a layer of its own
	Cmd            string // the binaries; no inner layer may import them

	// Handlers is where the HTTP handlers and routes live; it defaults to
	// Interfaces, else Infrastructure+"/http". Persistence is where the
	// repositories live and defaults to Infrastructure+"/db". Handlers must not
	// import Persistence.
	Handlers    string
	Persistence string
}

// Config describes the module being checked.
type Config struct {
	// Root is the module root (the directory of go.mod). Empty finds it from
	// the working directory upward, which is where a test runs.
	Root string

	Layers Layers

	// Allowed is the ratchet: per rule name, the files that may still break
	// the rule (ServiceRepositories: "file|domain/path", the domain path being
	// relative to Layers.Domain). A new violation and a stale entry both fail.
	Allowed map[string][]string

	// Skip switches rules off by name.
	Skip []string

	// Banned adds the application's own bans. They have no ratchet.
	Banned []Banned

	// Kernel is the folder of Layers.Domain that every aggregate may import
	// (the shared kernel) and that may carry json tags. Default "shared".
	Kernel string

	// SharedDomains are the domains whose repositories a service of any other
	// domain may inject (reference data). The Kernel is always shared.
	SharedDomains []string

	// OwnedDomains maps a folder of Layers.Application to the domain folders
	// it owns, when they are not just the same name.
	OwnedDomains map[string][]string

	// Inputs are the method names that make a type of Layers.Application an
	// input struct for BoundedStrings: it must have every one. Default
	// {"Validate"}.
	Inputs []string

	// ServiceType is the type-name suffix of the application services whose
	// exported methods ServiceContextFirst checks. Default "AppService".
	ServiceType string

	// Repository is the shape RepositoryShape holds the Persistence package to.
	// A field left empty leaves its part of the rule off.
	Repository Repository
}

// Repository names the one file of the repository package that may use a
// plumbing API of go-core, so every repository reaches it the same way.
type Repository struct {
	// Dir is the repository package, relative to the root; the rule checks the
	// production files of that package only.
	Dir string
	// TxFile is the only file that may call database.TxFromContext,
	// SQLTxFromContext, NewTransactor and NewTransactorFromRegistry.
	TxFile string
	// ScopeFile is the only file that may call tenancy.ScopeByTenant.
	ScopeFile string
}

// Banned is a rule of the application's own: an import, or one symbol of a
// package, that must not appear.
type Banned struct {
	// Import is the import path. It matches the path exactly, a trailing
	// "/..." matches the path and everything under it, and "..." alone matches
	// every package outside the standard library.
	Import string
	// Symbol, when set, bans only that member of the package (database.NewTransactor),
	// found by the name the file imports the package under.
	Symbol string
	// Why is the sentence a failure ends with: what to do instead.
	Why string

	// In limits the ban to these places, relative to the root: a file, a
	// package directory, or "dir/..." for a directory tree. Empty is the whole module.
	In []string
	// Except are places (as In) where the ban does not apply.
	Except []string
	// Unless are imports (as Import) a "..." or "/..." ban lets through.
	Unless []string
	// Tests also applies the ban to _test.go files and test-support packages
	// (a directory named *test). Default: production code only.
	Tests bool
}

// testPackage matches the directory of a test-support package (vehicletest,
// gocoretest): fixtures and doubles, where fail-fast panics and wall-clock
// reads are idiomatic.
var testPackage = regexp.MustCompile(`^[a-z0-9]+test$`)

// source is one parsed Go file.
type source struct {
	rel    string // relative to the root, forward slashes
	isTest bool
	file   *ast.File
}

type project struct {
	root   string
	module string
	fset   *token.FileSet
	files  []source
	cfg    Config
}

func (p *project) at(s source, n ast.Node) string {
	return fmt.Sprintf("%s:%d", s.rel, p.fset.Position(n.Pos()).Line)
}

// Run checks the module against cfg, one subtest per rule.
func Run(t *testing.T, cfg Config) {
	t.Helper()

	p, err := load(cfg)
	if err != nil {
		t.Fatalf("archtest: %v", err)
	}

	for _, r := range p.rules() {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()

			for _, problem := range r.check() {
				t.Error(problem)
			}
		})
	}
}

func load(cfg Config) (*project, error) {
	root := cfg.Root
	if root == "" {
		var err error
		if root, err = findRoot(); err != nil {
			return nil, err
		}
	}

	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}

	cfg.Layers = cfg.Layers.withDefaults(root)
	if cfg.Kernel == "" {
		cfg.Kernel = "shared"
	}

	if len(cfg.Inputs) == 0 {
		cfg.Inputs = []string{"Validate"}
	}

	if cfg.ServiceType == "" {
		cfg.ServiceType = "AppService"
	}

	p := &project{root: root, module: module, fset: token.NewFileSet(), cfg: cfg}

	if err := p.walk(); err != nil {
		return nil, err
	}

	return p, p.validate()
}

func (l Layers) withDefaults(root string) Layers {
	exists := func(dir string) bool {
		st, err := os.Stat(filepath.Join(root, dir))

		return err == nil && st.IsDir()
	}

	if l.Handlers == "" {
		switch {
		case l.Interfaces != "":
			l.Handlers = l.Interfaces
		case l.Infrastructure != "" && exists(l.Infrastructure+"/http"):
			l.Handlers = l.Infrastructure + "/http"
		}
	}

	if l.Persistence == "" && l.Infrastructure != "" && exists(l.Infrastructure+"/db") {
		l.Persistence = l.Infrastructure + "/db"
	}

	return l
}

func findRoot() (string, error) {
	dir, err := filepath.Abs(".")
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above the working directory: set Config.Root")
		}

		dir = parent
	}
}

func modulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			return strings.Trim(strings.TrimSpace(rest), `"`+"`"), nil
		}
	}

	return "", fmt.Errorf("no module line in %s/go.mod", root)
}

// walk parses every Go file of the module, leaving out testdata, vendor,
// node_modules, hidden and underscore folders and nested modules.
func (p *project) walk() error {
	return filepath.WalkDir(p.root, func(full string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			name := d.Name()
			if full != p.root {
				if name == "testdata" || name == "vendor" || name == "node_modules" || name[0] == '.' || name[0] == '_' {
					return filepath.SkipDir
				}

				if _, err := os.Stat(filepath.Join(full, "go.mod")); err == nil {
					return filepath.SkipDir // a module of its own
				}
			}

			return nil
		}

		if !strings.HasSuffix(full, ".go") {
			return nil
		}

		rel, err := filepath.Rel(p.root, full)
		if err != nil {
			return fmt.Errorf("relative path of %s: %w", full, err)
		}

		rel = filepath.ToSlash(rel)

		parsed, err := parser.ParseFile(p.fset, full, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", rel, err)
		}

		isTest := strings.HasSuffix(rel, "_test.go")
		for seg := range strings.SplitSeq(path.Dir(rel), "/") {
			isTest = isTest || testPackage.MatchString(seg)
		}

		p.files = append(p.files, source{rel: rel, isTest: isTest, file: parsed})

		return nil
	})
}

// validate refuses a configuration that would check nothing: a layer
// directory with no Go file, an unknown rule name.
func (p *project) validate() error {
	l := p.cfg.Layers
	for name, dir := range map[string]string{
		"Domain": l.Domain, "Application": l.Application, "Infrastructure": l.Infrastructure,
		"Interfaces": l.Interfaces, "Cmd": l.Cmd, "Handlers": l.Handlers, "Persistence": l.Persistence,
	} {
		if dir != "" && !slices.ContainsFunc(p.files, func(s source) bool { return inTree(s.rel, dir) }) {
			return fmt.Errorf("Layers.%s %q holds no Go file under %s: a guard that sees nothing passes forever", name, dir, p.root)
		}
	}

	for _, name := range p.cfg.Skip {
		if !slices.Contains(allRuleNames, name) {
			return fmt.Errorf("Skip names the unknown rule %q", name)
		}
	}

	for name := range p.cfg.Allowed {
		if !slices.Contains(allRuleNames, name) {
			return fmt.Errorf("Allowed names the unknown rule %q", name)
		}
	}

	return nil
}

var allRuleNames = []string{
	NoPanic, NoContextBackground, NoSleep, NoDatabaseSQL, ErrorfWrap, HandlersNoPersistence, LayerImports,
	DomainPurity, ApplicationPurity, NoTimeNowDomain, NoTimeNowApplication, CrossAggregate, DomainContextFirst,
	ServiceContextFirst, BoundedStrings, RoutePaths, RouteIdentifiers, ValidationRules, RouteAccess,
	ServiceRepositories, RepositoryShape,
}
