package archtest

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const module = "example.com/app"

// goodModule is a small module that breaks no rule; every case below changes
// it by one file.
func goodModule() map[string]string {
	return map[string]string{
		"go.mod": "module " + module + "\n\ngo 1.26\n",
		"internal/domain/shared/id.go": `package shared

type ID int
`,
		"internal/domain/order/order.go": `package order

import (
	"context"

	"example.com/app/internal/domain/shared"
)

type Order struct{ ID shared.ID }

type Repository interface {
	Find(ctx context.Context, id shared.ID) (*Order, error)
	Reader
}

type Reader interface{ Close(ctx context.Context) error }
`,
		"internal/domain/customer/customer.go": `package customer

type Customer struct{}

type Repository interface{ Find(ctx context.Context) }
`,
		"internal/application/order/service.go": `package order

import (
	"context"
	"fmt"

	"example.com/app/internal/domain/order"
)

type CreateInput struct {
	Name string ` + "`validation:\"required|max:50\"`" + `
}

func (CreateInput) Validate() error { return nil }

type AppService struct{ repo order.Repository }

func (s *AppService) Create(ctx context.Context, in CreateInput) error {
	if _, err := s.repo.Find(ctx, 1); err != nil {
		return fmt.Errorf("find: %w", err)
	}

	return nil
}

func (s *AppService) helper() {}
`,
		"internal/infrastructure/http/order/routes.go": `package order

import "example.com/app/internal/application/order"
import "github.com/wssto2/go-core/route"

const base = "/v1/orders"

var (
	Create = route.Post[order.CreateInput, route.None](base).Requires("sales.order:create")
	Show   = route.Get[route.None, route.None](base + "/:id/send-order").Authenticated()
	Health = route.Raw("GET", "/health").Public()
	_      = route.Group("order", Create, Show)
)
`,
		"internal/infrastructure/db/repository/base.go": `package repository

import "github.com/wssto2/go-core/database"

func tx() { _ = database.NewTransactor }
`,
		"internal/infrastructure/db/repository/order.go": `package repository

func find() {}
`,
		"cmd/app/main.go": `package main

func main() {}
`,
	}
}

func testConfig() Config {
	return Config{
		Layers: Layers{
			Domain: "internal/domain", Application: "internal/application", Infrastructure: "internal/infrastructure",
			Cmd: "cmd", Handlers: "internal/infrastructure/http", Persistence: "internal/infrastructure/db",
		},
		Repository: Repository{Dir: "internal/infrastructure/db/repository", TxFile: "base.go", ScopeFile: "scopes.go"},
		Banned:     []Banned{{Import: "example.com/legacy", Why: "gone"}},
	}
}

// problems writes the module with the changes applied (an empty content
// removes a file) and returns what the named rule reports.
func problems(t *testing.T, cfg Config, name string, changes map[string]string) []string {
	t.Helper()

	files := goodModule()
	for path, content := range changes {
		files[path] = content
	}

	root := t.TempDir()

	for path, content := range files {
		if content == "" {
			continue
		}

		full := filepath.Join(root, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}

	cfg.Root = root

	p, err := load(cfg)
	require.NoError(t, err)

	for _, r := range p.rules() {
		if r.name == name {
			return r.check()
		}
	}

	t.Fatalf("rule %q is not enabled", name)

	return nil
}

func TestAGoodModuleBreaksNoRule(t *testing.T) {
	t.Parallel()

	cfg := testConfig()

	for _, name := range allRuleNames {
		require.Empty(t, problems(t, cfg, name, nil), name)
	}
}

// Each case: the guard catches the violation, naming the file and the thing.
func TestGuardCatches(t *testing.T) {
	t.Parallel()

	tag := func(s string) string { return "`" + s + "`" }

	cases := []struct {
		rule    string
		changes map[string]string
		want    []string // substrings of the report
		not     []string // substrings it must not hold
	}{
		{NoPanic, map[string]string{
			"internal/domain/order/p.go":      "package order\n\nfunc f() { panic(\"x\") }\n",
			"internal/domain/order/p_test.go": "package order\n\nfunc g() { panic(\"x\") }\n",
		}, []string{"internal/domain/order/p.go:3  panic("}, []string{"p_test.go"}},
		{NoContextBackground, map[string]string{
			"internal/application/order/c.go": "package order\n\nimport \"context\"\n\nvar c = context.Background()\n",
		}, []string{"internal/application/order/c.go:5"}, nil},
		{NoSleep, map[string]string{
			"internal/application/order/s.go": "package order\n\nimport \"time\"\n\nfunc f() { time.Sleep(1) }\n",
		}, []string{"internal/application/order/s.go:5"}, nil},
		{NoDatabaseSQL, map[string]string{
			"internal/domain/order/sql.go": "package order\n\nimport _ \"database/sql\"\n",
		}, []string{"imports database/sql"}, nil},
		{ErrorfWrap, map[string]string{
			"internal/application/order/e.go": "package order\n\nimport \"fmt\"\n\nfunc f(err error) error { return fmt.Errorf(\"x: %v\", err) }\n",
		}, []string{"e.go:5  fmt.Errorf(...) passes err without %w"}, nil},
		{HandlersNoPersistence, map[string]string{
			"internal/infrastructure/http/order/h.go": "package order\n\nimport _ \"example.com/app/internal/infrastructure/db/repository\"\n",
		}, []string{"h.go:3  imports example.com/app/internal/infrastructure/db/repository"}, nil},
		{LayerImports, map[string]string{
			"internal/domain/order/l.go":           "package order\n\nimport _ \"example.com/app/internal/application/order\"\n",
			"internal/domain/order/l_test.go":      "package order\n\nimport _ \"example.com/app/internal/infrastructure/db/repository\"\n",
			"internal/application/order/l.go":      "package order\n\nimport _ \"example.com/app/internal/infrastructure/http/order\"\n",
			"internal/application/order/l_cmd.go":  "package order\n\nimport _ \"example.com/app/cmd/app\"\n",
			"internal/application/order/l_dom.go":  "package order\n\nimport _ \"example.com/app/internal/domain/customer\"\n",
			"internal/infrastructure/http/ok_.go":  "package http\n\nimport _ \"example.com/app/internal/application/order\"\n",
			"internal/infrastructure/http/ok2_.go": "package http\n\nimport _ \"example.com/app/internal/domain/order\"\n",
		}, []string{"internal/domain/order/l.go:3", "internal/domain/order/l_test.go:3", "internal/application/order/l.go:3", "l_cmd.go:3"},
			[]string{"l_dom.go", "ok_.go", "ok2_.go"}},
		{DomainPurity, map[string]string{
			"internal/domain/order/g.go":   "package order\n\nimport _ \"github.com/gin-gonic/gin\"\nimport _ \"encoding/json\"\nimport _ \"gorm.io/gorm/clause\"\nimport _ \"github.com/wssto2/go-core/datatable\"\n",
			"internal/domain/order/t.go":   "package order\n\ntype T struct {\n\tA int " + tag(`json:"a"`) + "\n}\n",
			"internal/domain/shared/t.go":  "package shared\n\ntype T struct {\n\tA int " + tag(`json:"a"`) + "\n}\n",
			"internal/domain/order/ok.go":  "package order\n\ntype U struct {\n\tA int " + tag(`gorm:"a"`) + "\n}\n",
			"internal/domain/order/ok2.go": "package order\n\nimport _ \"errors\"\n",
		}, []string{"g.go:3  imports github.com/gin-gonic/gin", "g.go:4  imports encoding/json", "g.go:5  imports gorm.io/gorm/clause",
			"g.go:6  imports github.com/wssto2/go-core/datatable", "t.go:4  has a json: tag"}, []string{"shared/t.go", "ok.go", "ok2.go"}},
		{ApplicationPurity, map[string]string{
			"internal/application/order/g.go": "package order\n\nimport _ \"github.com/gin-gonic/gin\"\nimport _ \"gorm.io/gorm\"\nimport _ \"database/sql\"\n",
		}, []string{"g.go:3  imports github.com/gin-gonic/gin", "g.go:4  imports gorm.io/gorm", "g.go:5  imports database/sql"}, nil},
		{NoTimeNowDomain, map[string]string{
			"internal/domain/order/n.go": "package order\n\nimport \"time\"\n\nvar a = time.Now()\n\nvar b = struct{ now func() time.Time }{now: time.Now}\n",
		}, []string{"n.go:5", "n.go:7"}, nil},
		{NoTimeNowApplication, map[string]string{
			"internal/application/order/n.go": "package order\n\nimport \"time\"\n\nvar a = time.Now()\n",
		}, []string{"n.go:5"}, nil},
		{CrossAggregate, map[string]string{
			"internal/domain/order/x.go":  "package order\n\nimport _ \"example.com/app/internal/domain/customer\"\n",
			"internal/domain/order/y.go":  "package order\n\nimport _ \"example.com/app/internal/domain/shared\"\n",
			"internal/domain/order/z.go":  "package order\n\nimport _ \"example.com/app/internal/domain/order\"\n",
			"internal/domain/top_file.go": "package domain\n\nimport _ \"example.com/app/internal/domain/customer\"\n",
		}, []string{"x.go:3  imports the customer aggregate"}, []string{"y.go", "z.go", "top_file.go"}},
		{DomainContextFirst, map[string]string{
			"internal/domain/order/p.go": "package order\n\ntype Port interface {\n\tGet(id int) error\n\tOk(ctx context.Context, id int)\n}\n",
		}, []string{"p.go:4  Port.Get does not take context.Context first"}, []string{"Port.Ok"}},
		{ServiceContextFirst, map[string]string{
			"internal/application/order/s.go": "package order\n\ntype OfferAppService struct{}\n\nfunc (s *OfferAppService) Do(id int) {}\n\nfunc (s *OfferAppService) hidden(id int) {}\n\ntype Other struct{}\n\nfunc (Other) Do(id int) {}\n",
		}, []string{"s.go:5  OfferAppService.Do does not take context.Context first"}, []string{"hidden", "Other"}},
		{BoundedStrings, map[string]string{
			"internal/application/order/in.go": "package order\n\ntype Update struct {\n\tTitle string " + tag(`validation:"required"`) + "\n\tNote  *string " + tag(`validation:"max:5"`) + "\n\tLine  Line\n\tTags []string\n}\n\nfunc (Update) Validate() error { return nil }\n\ntype Line struct {\n\tText string\n}\n\ntype Filter struct{ Q string }\n",
		}, []string{"in.go:4  Update.Title (string) has no max:", "in.go:13  Line.Text (string) has no max:"}, []string{"Note", "Filter", "Tags"}},
		{RoutePaths, map[string]string{
			"internal/infrastructure/http/order/p.go": "package order\n\nimport \"github.com/wssto2/go-core/route\"\n\nvar (\n\tA = route.Post[int, int](base + \"/sendOrder\")\n\tB = route.Delete[int, int](\"/v1/price_rules/:id\")\n\tC = route.Raw(\"GET\", \"/v1/Files\")\n\tD = route.Raw(\"GET\", \"/v1/files/*path\")\n\tE = route.Post[int, int](base + \"/stepper/1\")\n\tF = route.Post[int, int](base + \"/stepper/Step1\")\n\tG = route.Post[int, int](dynamic())\n\tH = route.Name(\"not a route\")\n)\n",
		}, []string{`segment "sendOrder"`, `segment "price_rules"`, `segment "Files"`, `segment "Step1"`, "not a string literal or constants"},
			[]string{`"1"`, `"files"`, `"path"`, `"not a route"`}},
		{RouteIdentifiers, map[string]string{
			"internal/infrastructure/http/order/i.go": "package order\n\nimport \"github.com/wssto2/go-core/route\"\n\nvar (\n\tA = route.Get[int, int](\"/a\").Requires(\"sales_plan.order:view_all\")\n\tB = route.Group(\"sales-plan\")\n\tC = route.Group(\"salesplan\")\n\tD = route.Get[int, int](\"/b\").RequiresAny(\"ok.fine:view\", \"x.Y:view\")\n)\n",
		}, []string{`permission namespace component "sales_plan"`, `permission verb "view_all"`, `route group segment "sales-plan"`, `component "Y"`},
			[]string{`"salesplan"`, `"ok"`, `"fine"`}},
		{ValidationRules, map[string]string{
			"internal/application/order/v.go": "package order\n\ntype V struct {\n\tA int " + tag(`validation:"gte=0"`) + "\n\tB string " + tag(`validation:"required|max:3"`) + "\n}\n",
		}, []string{"v.go:4", "gte=0"}, []string{"v.go:5"}},
		{RouteAccess, map[string]string{
			"internal/infrastructure/http/order/a.go": "package order\n\nimport rt \"github.com/wssto2/go-core/route\"\n\nvar (\n\tA = rt.Get[int, int](base).Name(\"x\")\n\tB = rt.Raw(\"POST\", base+\"/raw\").To(nil)\n\tC = rt.Put[int, int](base).Name(\"c\").Requires(\"a.b:c\").To(nil)\n\tD = rt.Get[int, int](base).PersonOnly()\n\tE = rt.Get[int, int](base).Name(\"e\").Authenticated().Status(201)\n\tF = rt.Patch[int, int](base).RequiresAll(\"a.b:c\")\n\tG = rt.Delete[int, int](base).Public()\n)\n",
		}, []string{"a.go:6  GET /v1/orders declares no access", "a.go:7  POST /v1/orders/raw declares no access"}, []string{"a.go:8", "a.go:9", "a.go:10", "a.go:11", "a.go:12"}},
		{ServiceRepositories, map[string]string{
			"internal/application/order/r.go":  "package order\n\nimport \"example.com/app/internal/domain/customer\"\n\ntype S struct{ r customer.Repository }\n",
			"internal/application/order/r2.go": "package order\n\nimport \"example.com/app/internal/domain/customer\"\n\nvar _ customer.Customer\n",
			"internal/application/order/r3.go": "package order\n\nimport c2 \"example.com/app/internal/domain/customer\"\n\ntype T struct{ r c2.Repository }\n",
		}, []string{"r.go injects a Repository from internal/domain/customer", "r3.go injects"}, []string{"r2.go"}},
		{RepositoryShape, map[string]string{
			"internal/infrastructure/db/repository/bad.go":    "package repository\n\nimport (\n\tdb \"github.com/wssto2/go-core/database\"\n\t\"github.com/wssto2/go-core/tenancy\"\n)\n\nfunc f() {\n\t_ = db.TxFromContext\n\t_ = tenancy.ScopeByTenant\n}\n",
			"internal/infrastructure/db/repository/scopes.go": "package repository\n\nimport \"github.com/wssto2/go-core/tenancy\"\n\nvar _ = tenancy.ScopeByTenant\n",
		}, []string{"bad.go:9  uses db.TxFromContext", "bad.go:10  uses tenancy.ScopeByTenant"}, []string{"base.go", "scopes.go"}},
	}

	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			t.Parallel()

			report := strings.Join(problems(t, testConfig(), tc.rule, tc.changes), "\n")
			require.NotEmpty(t, report, "the guard caught nothing")

			for _, want := range tc.want {
				require.Contains(t, report, want)
			}

			for _, not := range tc.not {
				require.NotContains(t, report, not)
			}
		})
	}
}

func TestBanned(t *testing.T) {
	t.Parallel()

	run := func(b Banned, changes map[string]string) string {
		cfg := testConfig()
		cfg.Banned = []Banned{b}

		return strings.Join(problems(t, cfg, "banned", changes), "\n")
	}

	files := map[string]string{
		"internal/application/order/b.go":      "package order\n\nimport (\n\t\"example.com/legacy\"\n\t\"github.com/wssto2/go-core/auth\"\n)\n\nvar _ = legacy.X\nvar _ = auth.Authorize\nvar _ = auth.User\n",
		"internal/application/order/b_test.go": "package order\n\nimport _ \"example.com/legacy\"\n",
	}

	got := run(Banned{Import: "example.com/legacy", Why: "removed"}, files)
	require.Contains(t, got, "BANNED (example.com/legacy)")
	require.Contains(t, got, "b.go:4  imports example.com/legacy")
	require.Contains(t, got, "-> removed")
	require.NotContains(t, got, "b_test.go")

	require.Contains(t, run(Banned{Import: "example.com/legacy", Tests: true, Why: "removed"}, files), "b_test.go:3")

	got = run(Banned{Import: "github.com/wssto2/go-core/auth", Symbol: "Authorize", Why: "gone"}, files)
	require.Contains(t, got, "b.go:9  uses auth.Authorize")
	require.NotContains(t, got, "auth.User")

	require.Empty(t, run(Banned{Import: "example.com/legacy", Except: []string{"internal/application/order/b.go"}, Why: "x"}, files))
	require.Empty(t, run(Banned{Import: "example.com/legacy", In: []string{"internal/domain/..."}, Why: "x"}, files))
	require.Contains(t, run(Banned{Import: "example.com/legacy", In: []string{"internal/application/order"}, Why: "x"}, files), "b.go")
	require.Empty(t, run(Banned{Import: "example.com/legacy", In: []string{"internal/application"}, Why: "x"}, files), "a bare directory is that package only")

	got = run(Banned{Import: "...", Unless: []string{"github.com/wssto2/go-core/..."}, Why: "closed"}, files)
	require.Contains(t, got, "imports example.com/legacy")
	require.NotContains(t, got, "go-core/auth")
	require.NotContains(t, got, "imports context", "the standard library is not matched by ...")
}

func TestRatchet(t *testing.T) {
	t.Parallel()

	found := []finding{{"a.go", "a.go:1  bad"}, {"a.go", "a.go:9  bad"}, {"b.go", "b.go:2  bad"}}

	require.Empty(t, settle("t", "h", []string{"a.go", "b.go"}, found))

	fresh := strings.Join(settle("t", "h", []string{"a.go"}, found), "\n")
	require.Contains(t, fresh, "CONVENTION VIOLATION (t)")
	require.Contains(t, fresh, "b.go:2  bad")
	require.NotContains(t, fresh, "a.go:1")

	stale := strings.Join(settle("t", "h", []string{"a.go", "b.go", "gone.go"}, found), "\n")
	require.Contains(t, stale, "STALE ALLOWED ENTRY (t)")
	require.Contains(t, stale, "gone.go")
	require.NotContains(t, stale, "CONVENTION VIOLATION")
}

func TestAllowedFreezesAFileAndGoesStale(t *testing.T) {
	t.Parallel()

	bad := map[string]string{"internal/domain/order/n.go": "package order\n\nimport \"time\"\n\nvar a = time.Now()\n"}
	cfg := testConfig()
	cfg.Allowed = map[string][]string{NoTimeNowDomain: {"internal/domain/order/n.go"}}

	require.Empty(t, problems(t, cfg, NoTimeNowDomain, bad))
	require.Contains(t, strings.Join(problems(t, cfg, NoTimeNowDomain, nil), ""), "STALE ALLOWED ENTRY")
}

func TestServiceRepositoriesKeysAndOwnership(t *testing.T) {
	t.Parallel()

	changes := map[string]string{
		"internal/application/order/r.go": "package order\n\nimport \"example.com/app/internal/domain/customer\"\n\ntype S struct{ r customer.Repository }\n",
	}

	cfg := testConfig()
	cfg.Allowed = map[string][]string{ServiceRepositories: {"internal/application/order/r.go|customer"}}
	require.Empty(t, problems(t, cfg, ServiceRepositories, changes))

	cfg = testConfig()
	cfg.OwnedDomains = map[string][]string{"order": {"order", "customer"}}
	require.Empty(t, problems(t, cfg, ServiceRepositories, changes))

	cfg = testConfig()
	cfg.SharedDomains = []string{"customer"}
	require.Empty(t, problems(t, cfg, ServiceRepositories, changes))
}

func TestBoundedStringsInputMethods(t *testing.T) {
	t.Parallel()

	changes := map[string]string{
		"internal/application/order/in.go": "package order\n\ntype Both struct{ A string }\n\nfunc (Both) Validate() error { return nil }\nfunc (Both) Sanitize() {}\n\ntype One struct{ B string }\n\nfunc (One) Validate() error { return nil }\n",
	}

	cfg := testConfig()
	cfg.Inputs = []string{"Validate", "Sanitize"}

	got := strings.Join(problems(t, cfg, BoundedStrings, changes), "")
	require.Contains(t, got, "Both.A")
	require.NotContains(t, got, "One.B")
	require.Contains(t, strings.Join(problems(t, testConfig(), BoundedStrings, changes), ""), "One.B")
}

func TestConfigErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module "+module+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644))

	_, err := load(Config{Root: root, Layers: Layers{Domain: "internal/domain"}})
	require.ErrorContains(t, err, "Layers.Domain")

	_, err = load(Config{Root: root, Allowed: map[string][]string{"no-such-rule": {"x.go"}}})
	require.ErrorContains(t, err, `unknown rule "no-such-rule"`)

	_, err = load(Config{Root: root, Skip: []string{"no-such-rule"}})
	require.ErrorContains(t, err, "unknown rule")

	p, err := load(Config{Root: root, Skip: []string{ErrorfWrap}})
	require.NoError(t, err)
	require.False(t, slices.ContainsFunc(p.rules(), func(r rule) bool { return r.name == ErrorfWrap }))
	require.Equal(t, module, p.module)
}

// The walk leaves out what the go tool leaves out, and a module of its own.
func TestWalkSkips(t *testing.T) {
	t.Parallel()

	p := loadFiles(t, map[string]string{
		"go.mod": "module " + module + "\n", "a.go": "package a\n", "testdata/b.go": "package b\n", "vendor/c.go": "package c\n",
		".claude/worktrees/x/d.go": "package d\n", "sub/go.mod": "module other\n", "sub/e.go": "package e\n",
		"fixturetest/f.go": "package f\n", "g_test.go": "package a\n",
	})

	got := map[string]bool{}
	for _, s := range p.files {
		got[s.rel] = s.isTest
	}

	require.Equal(t, map[string]bool{"a.go": false, "fixturetest/f.go": true, "g_test.go": true}, got)
}

func loadFiles(t *testing.T, files map[string]string) *project {
	t.Helper()

	root := t.TempDir()

	for path, content := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
	}

	p, err := load(Config{Root: root})
	require.NoError(t, err)

	return p
}

// Run reports a clean module as passing subtests.
func TestRunPassesAGoodModule(t *testing.T) {
	root := t.TempDir()

	for path, content := range goodModule() {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
	}

	cfg := testConfig()
	cfg.Root = root
	Run(t, cfg)
}
