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
//   - SQLPortability (opt-in, Config.SQL): no SQL literal or gorm locking clause in the
//     Go sources that MariaDB 10.3 rejects;
//   - RoutePersonOnly (opt-in, Config.PersonOnly): every route in an area of the API that
//     only a person may use declares PersonOnly or Public, and every area has a route;
//   - ServiceRepositories: a service injects only its own aggregate's repositories;
//   - RepositoryShape: the transaction and tenant-scope plumbing of go-core is
//     reached from one file of the repository package only.
//
// LogicIDs is a guard of its own, not a rule of Run: it holds the code to a
// registry of Logic IDs (every ID the mapping declares is named in a comment).
//
// A rule whose layer is not configured is not run. Banned adds the rules of the
// application itself (a removed API, a package one layer must not import), and
// an application's own guards walk the same parsed files with Sources and
// freeze their violations with Ratchet.
//
// Pre-existing violations are frozen in a ratchet: Config.Allowed lists, per
// rule, the files (for ServiceRepositories "file|domain/path") that may still
// break it. A new violation fails the test, and so does a stale entry, so the
// list only ever shrinks.
//
//	func TestArchitecture(t *testing.T) {
//		var cfg archtest.Config
//		cfg.Layers.Domain = "internal/domain"
//		cfg.Layers.Application = "internal/application"
//		cfg.Layers.Infrastructure = "internal/infrastructure"
//		cfg.Allowed = map[string][]string{
//			archtest.NoTimeNowDomain: {"internal/domain/shared/clock.go"},
//		}
//		cfg.SharedDomains = []string{"locale", "settings"}
//		cfg.Banned = []archtest.Banned{{
//			Import: "example.com/legacy/gate",
//			Why:    "routes are gated by Requires; the gate is gone",
//		}}
//
//		archtest.Run(t, cfg)
//	}
package archtest

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

var errConfig = errors.New("archtest: bad configuration")

// Run checks the module against cfg, one parallel subtest per rule.
func Run(t *testing.T, cfg Config) {
	t.Helper()

	proj, err := load(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for _, found := range proj.rules() {
		t.Run(found.name, func(t *testing.T) {
			t.Parallel()

			for _, problem := range found.check() {
				t.Error(problem)
			}
		})
	}
}

// Result is what one rule reported.
type Result struct {
	Rule     string
	Problems []string // empty when the rule holds
}

// Check runs the same rules as Run and returns their results instead of
// failing a test: one Result for every rule that ran.
func Check(cfg Config) ([]Result, error) {
	proj, err := load(cfg)
	if err != nil {
		return nil, err
	}

	var out []Result

	for _, found := range proj.rules() {
		out = append(out, Result{Rule: found.name, Problems: found.check()})
	}

	return out, nil
}

// Sources returns every Go file of the module at root ("" finds the module
// from the working directory), without testdata, vendor, node_modules, hidden
// and underscore folders and nested modules. It parses on every call: a test
// package with several guards calls it once and shares the result.
func Sources(t *testing.T, root string) []Source {
	t.Helper()

	abs, err := resolveRoot(root)
	if err != nil {
		t.Fatal(err)
	}

	files, err := walk(abs)
	if err != nil {
		t.Fatal(err)
	}

	return files
}

// Ratchet settles an application's own rule against its frozen list, as Run
// does for the built-in ones: byFile maps a file to its violations ("file:line
// text"). A file not in allowed fails the test with the violations and the
// hint, and so does an allowed entry with no violation.
func Ratchet(t *testing.T, title, hint string, allowed []string, byFile map[string][]string) {
	t.Helper()

	var found []finding

	for file, texts := range byFile {
		for _, text := range texts {
			found = append(found, finding{key: file, text: text})
		}
	}

	for _, problem := range settle(title, hint, allowed, found) {
		t.Error(problem)
	}
}

type project struct {
	module string
	files  []Source
	cfg    Config
}

func load(cfg Config) (*project, error) {
	root, err := resolveRoot(cfg.Root)
	if err != nil {
		return nil, err
	}

	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}

	cfg.Layers = cfg.Layers.withDefaults(root)
	cfg.Kernel = orDefault(cfg.Kernel, "shared")
	cfg.ServiceType = orDefault(cfg.ServiceType, "AppService")

	if len(cfg.Inputs) == 0 {
		cfg.Inputs = []string{"Validate"}
	}

	files, err := walk(root)
	if err != nil {
		return nil, err
	}

	proj := &project{module: module, files: files, cfg: cfg}

	return proj, proj.validate(root)
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

// validate refuses a configuration that would check nothing: a layer
// directory with no Go file, an unknown rule name.
func (p *project) validate(root string) error {
	for name, dir := range p.cfg.Layers.named() {
		if dir != "" && !slices.ContainsFunc(p.files, func(src Source) bool { return inTree(src.Rel, dir) }) {
			return fmt.Errorf("%w: Layers.%s %q holds no Go file under %s (a guard that sees nothing passes forever)",
				errConfig, name, dir, root)
		}
	}

	known := ruleNames()

	for _, name := range p.cfg.Skip {
		if !slices.Contains(known, name) {
			return fmt.Errorf("%w: Skip names the unknown rule %q", errConfig, name)
		}
	}

	for name := range p.cfg.Allowed {
		if !slices.Contains(known, name) {
			return fmt.Errorf("%w: Allowed names the unknown rule %q", errConfig, name)
		}
	}

	return nil
}
