package archtest_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/archtest"
)

const (
	legacyImport = "example.com/legacy"
	orderDomain  = "internal/domain/order"
	clockFile    = "internal/domain/order/clock.go"
	allowedFixt  = "allowed"
	customerDom  = "customer"
	orderApp     = "internal/application/order"
	mainFile     = "cmd/app/main.go"
)

// fixture is the good module of testdata/good, which breaks no rule, with the
// files of the named testdata/bad folders added; it returns the root.
func fixture(t *testing.T, overlays ...string) string {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS(filepath.Join("testdata", "good"))))

	for _, name := range overlays {
		require.NoError(t, os.CopyFS(root, os.DirFS(filepath.Join("testdata", "bad", name))))
	}

	return root
}

func testConfig(root string) archtest.Config {
	var cfg archtest.Config

	cfg.Root = root
	cfg.Layers.Domain = "internal/domain"
	cfg.Layers.Application = "internal/application"
	cfg.Layers.Infrastructure = "internal/infrastructure"
	cfg.Layers.Cmd = "cmd"
	cfg.Layers.Handlers = "internal/infrastructure/http"
	cfg.Layers.Persistence = "internal/infrastructure/db"
	cfg.Repository.Dir = "internal/infrastructure/db/repository"
	cfg.Repository.TxFile = "base.go"
	cfg.Repository.ScopeFile = "scopes.go"
	cfg.Banned = []archtest.Banned{banned(legacyImport, "", "gone")}

	return cfg
}

// banned builds a ban without its optional places.
func banned(importPath, symbol, why string) archtest.Banned {
	var ban archtest.Banned

	ban.Import = importPath
	ban.Symbol = symbol
	ban.Why = why

	return ban
}

// problems is what the rule reports on the module of the config, joined; it fails when the rule did not run.
func problems(t *testing.T, cfg archtest.Config, rule string) string {
	t.Helper()

	results, err := archtest.Check(cfg)
	require.NoError(t, err)

	for _, result := range results {
		if result.Rule == rule {
			return strings.Join(result.Problems, "\n")
		}
	}

	t.Fatalf("rule %q did not run", rule)

	return ""
}

// write puts a file in a scratch module.
func write(t *testing.T, root, rel, content string) {
	t.Helper()

	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
}

func allRules() []string {
	return []string{
		archtest.NoPanic, archtest.NoContextBackground, archtest.NoSleep, archtest.NoDatabaseSQL, archtest.ErrorfWrap,
		archtest.HandlersNoPersistence, archtest.LayerImports, archtest.DomainPurity, archtest.ApplicationPurity,
		archtest.NoTimeNowDomain, archtest.NoTimeNowApplication, archtest.CrossAggregate, archtest.DomainContextFirst,
		archtest.ServiceContextFirst, archtest.BoundedStrings, archtest.ValidationRules, archtest.ServiceRepositories,
		archtest.RepositoryShape, archtest.RoutePaths, archtest.RouteIdentifiers, archtest.RouteAccess, "banned",
	}
}

func TestAGoodModuleBreaksNoRule(t *testing.T) {
	t.Parallel()

	results, err := archtest.Check(testConfig(fixture(t)))
	require.NoError(t, err)

	ran := make([]string, 0, len(results))

	for _, result := range results {
		ran = append(ran, result.Rule)

		require.Empty(t, result.Problems, result.Rule)
	}

	slices.Sort(ran)

	want := allRules()
	slices.Sort(want)
	require.Equal(t, want, ran, "every rule ran")
}

func TestRunPassesAGoodModule(t *testing.T) {
	t.Parallel()

	archtest.Run(t, testConfig(fixture(t)))
}

func TestAllowedFreezesAFileAndGoesStale(t *testing.T) {
	t.Parallel()

	cfg := testConfig(fixture(t, allowedFixt))
	require.Contains(t, problems(t, cfg, archtest.NoTimeNowDomain), clockFile+":5")

	cfg.Allowed = map[string][]string{archtest.NoTimeNowDomain: {clockFile}}
	require.Empty(t, problems(t, cfg, archtest.NoTimeNowDomain))

	cfg = testConfig(fixture(t))
	cfg.Allowed = map[string][]string{archtest.NoTimeNowDomain: {clockFile}}

	got := problems(t, cfg, archtest.NoTimeNowDomain)
	require.Contains(t, got, "STALE ALLOWED ENTRY")
	require.Contains(t, got, clockFile)
	require.NotContains(t, got, "CONVENTION VIOLATION")
}

func TestServiceRepositoriesKeysAndOwnership(t *testing.T) {
	t.Parallel()

	root := fixture(t, "service-repositories")
	key := orderApp + "/injects.go|" + customerDom
	aliasKey := orderApp + "/aliased.go|" + customerDom

	cfg := testConfig(root)
	cfg.Allowed = map[string][]string{archtest.ServiceRepositories: {key, aliasKey}}
	require.Empty(t, problems(t, cfg, archtest.ServiceRepositories))

	cfg = testConfig(root)
	cfg.OwnedDomains = map[string][]string{"order": {"order", customerDom}}
	require.Empty(t, problems(t, cfg, archtest.ServiceRepositories))

	cfg = testConfig(root)
	cfg.SharedDomains = []string{customerDom}
	require.Empty(t, problems(t, cfg, archtest.ServiceRepositories))
}

func TestBoundedStringsInputMethods(t *testing.T) {
	t.Parallel()

	root := fixture(t, "bounded-strings-sanitize")

	cfg := testConfig(root)
	cfg.Inputs = []string{"Validate", "Sanitize"}

	got := problems(t, cfg, archtest.BoundedStrings)
	require.Contains(t, got, "Both.First")
	require.NotContains(t, got, "One.Second")
	require.Contains(t, problems(t, testConfig(root), archtest.BoundedStrings), "One.Second")
}

func TestConfigErrors(t *testing.T) {
	t.Parallel()

	root := fixture(t)

	cfg := testConfig(root)
	cfg.Layers.Domain = "internal/missing"
	_, err := archtest.Check(cfg)
	require.ErrorContains(t, err, "Layers.Domain")

	cfg = testConfig(root)
	cfg.Allowed = map[string][]string{"no-such-rule": {"x.go"}}
	_, err = archtest.Check(cfg)
	require.ErrorContains(t, err, `unknown rule "no-such-rule"`)

	cfg = testConfig(root)
	cfg.Skip = []string{"no-such-rule"}
	_, err = archtest.Check(cfg)
	require.ErrorContains(t, err, "unknown rule")

	cfg = testConfig(root)
	cfg.Skip = []string{archtest.ErrorfWrap, "banned"}
	results, err := archtest.Check(cfg)
	require.NoError(t, err)
	require.Len(t, results, len(allRules())-2)
}

// The walk leaves out what the go tool leaves out, and a module of its own.
func TestSourcesSkipWhatTheGoToolSkips(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, rel := range []string{
		"go.mod", "a.go", "testdata/b.go", "vendor/c.go", ".claude/worktrees/x/d.go", "node_modules/m/n.go",
		"sub/go.mod", "sub/e.go", "fixturetest/f.go", "g_test.go",
	} {
		write(t, root, rel, "package a\n")
	}

	got := map[string]bool{}
	for _, src := range archtest.Sources(t, root) {
		got[src.Rel] = src.IsTest
	}

	require.Equal(t, map[string]bool{"a.go": false, "fixturetest/f.go": true, "g_test.go": true}, got)
}

func TestSourcesAndRatchetServeAnApplicationsOwnGuards(t *testing.T) {
	t.Parallel()

	byFile := map[string][]string{}

	for _, src := range archtest.Sources(t, fixture(t)) {
		if src.Rel == mainFile {
			byFile[src.Rel] = append(byFile[src.Rel], src.At(src.File.Decls[0])+"  func")
		}
	}

	require.Equal(t, map[string][]string{mainFile: {mainFile + ":3  func"}}, byFile)

	archtest.Ratchet(t, "frozen", "hint", []string{mainFile}, byFile)
}

func TestSQLPortabilityRule(t *testing.T) {
	t.Parallel()

	const locks = "internal/infrastructure/db/repository/locks.go"

	// Opt-in: a module that configures no directory is not checked.
	cfg := testConfig(fixture(t, "sql-portability"))
	results, err := archtest.Check(cfg)
	require.NoError(t, err)

	for _, result := range results {
		require.NotEqual(t, archtest.SQLPortability, result.Rule)
	}

	cfg.SQL.Dirs = []string{"internal"}
	got := problems(t, cfg, archtest.SQLPortability)

	// Lines 6-15 are the violations, 16-19 are what MariaDB 10.3 accepts.
	for line := 6; line <= 15; line++ {
		require.Contains(t, got, locks+":"+strconv.Itoa(line)+"  ", "line %d", line)
	}

	for line := 16; line <= 19; line++ {
		require.NotContains(t, got, locks+":"+strconv.Itoa(line)+"  ", "line %d", line)
	}

	require.NotContains(t, got, "locks_test.go", "test files are not production SQL")

	cfg.SQL.Dirs = []string{"cmd"}
	require.Empty(t, problems(t, cfg, archtest.SQLPortability), "only the configured directories are scanned")

	cfg.SQL.Dirs = []string{"internal"}
	cfg.Allowed = map[string][]string{archtest.SQLPortability: {locks}}
	require.Empty(t, problems(t, cfg, archtest.SQLPortability))

	require.Empty(t, problems(t, withSQL(testConfig(fixture(t))), archtest.SQLPortability), "a good module is clean")
}

func withSQL(cfg archtest.Config) archtest.Config {
	cfg.SQL.Dirs = []string{"internal", "cmd"}

	return cfg
}
