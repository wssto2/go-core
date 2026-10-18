package gocore_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/go2ts"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/route"
)

const (
	ticketRoutes  = "routes/tickets/routes.ts"
	tsGenerate    = "ts:generate"
	permissionsTS = "permissions.ts"
	emptyTS       = "export {}\n"
)

type tsTicket struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type tsPerson struct {
	Name string `json:"name"`
}

type tsShowInput struct {
	ID int `path:"id"`
}

type tsCloseInput struct {
	Reason string `json:"reason" validation:"max:100"`
}

type tsLabel struct {
	Text string `json:"text"`
}

// tsBare is an App with the quiet logger and nothing else but opts.
func tsBare(opts ...gocore.Option) *gocore.App {
	quiet := gocore.WithLogger(newAccessApp().Logger())

	return gocore.New(bootstrap.DefaultConfig(), append([]gocore.Option{quiet}, opts...)...)
}

// tsNoLists asks for ts:generate with no lists of types, writing under dir.
func tsNoLists(dir string) []gocore.Option {
	lists := gocore.TS{Entities: nil, Inputs: nil, Dir: dir}

	return []gocore.Option{gocore.WithArgs(tsGenerate), gocore.WithTypeScript(lists)}
}

func tsTicketHandler(context.Context, tsShowInput) (tsTicket, error) {
	return tsTicket{ID: 0, Title: ""}, nil
}
func tsPersonHandler(context.Context, route.None) (tsPerson, error) { return tsPerson{Name: ""}, nil }

// tsMember stands for a go-core module (identity, access, notification): it brings its own route group, marked
// with Shipping like the real ones.
func tsMember(app *gocore.App) {
	defer app.Shipping()()

	people := route.Get[route.None, tsPerson]("/people").Authenticated()
	_ = route.Group("people", people)

	app.Routes(people.To(tsPersonHandler))
}

func tsPermissions() *authz.Catalogue {
	c := authz.NewCatalogue()
	c.MustDefine("tickets.ticket:view")

	return c
}

// tsFeature is the application's own: a group, and a catalogue it hands over.
func tsFeature(app *gocore.App, catalogue *authz.Catalogue) {
	show := route.Get[tsShowInput, tsTicket]("/tickets/:id").Requires("tickets.ticket:view")
	_ = route.Group("tickets", show)

	app.Routes(show.To(tsTicketHandler))
	app.Permissions(catalogue)
}

// tsApp is an application as main builds it, with the command line args.
func tsApp(dir string, args []string, opts ...gocore.Option) *gocore.App {
	base := make([]gocore.Option, 0, 3+len(opts))
	base = append(base,
		gocore.WithArgs(args...),
		gocore.WithLogger(newAccessApp().Logger()),
		gocore.WithTypeScript(gocore.TS{
			Entities: []any{go2ts.As(tsLabel{Text: ""}, "Label")},
			Inputs:   []any{tsCloseInput{Reason: ""}},
			Dir:      dir,
		}),
	)

	return gocore.New(bootstrap.DefaultConfig(), append(base, opts...)...)
}

// tsTree lists every file under dir, relative and slash-separated, with its content.
func tsTree(t *testing.T, dir string) map[string]string {
	t.Helper()

	tree := map[string]string{}

	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		data, err := os.ReadFile(path) //nolint:gosec // a test's own temp directory
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		rel, _ := filepath.Rel(dir, path)
		tree[filepath.ToSlash(rel)] = string(data)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return tree
}

// writeTS creates the file, and the folders above it.
func writeTS(t *testing.T, path, body string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(path, []byte(body), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func tsNames(tree map[string]string) []string {
	names := make([]string, 0, len(tree))
	for name := range tree {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// tsMustRun is tsRun for a command that must succeed.
func tsMustRun(t *testing.T, app *gocore.App, args ...string) {
	t.Helper()

	_, err := tsRun(t, app, args...)
	if err != nil {
		t.Fatal(err)
	}
}

func tsRun(t *testing.T, app *gocore.App, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer

	err := app.RunCommand(t.Context(), args, &out)

	return out.String(), err
}

func TestTypeScriptWritesTheApplicationsPartsOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	app := tsApp(dir, []string{tsGenerate})

	tsMember(app)
	tsFeature(app, tsPermissions())

	if got := len(app.Contracts()); got != 2 {
		t.Fatalf("both groups are installed, got %d", got)
	}

	_, err := tsRun(t, app, tsGenerate)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"entities/Label.ts", permissionsTS, "routes/tickets/entities.ts", ticketRoutes,
		"routes/tickets/schemas.ts", "schemas/tsCloseInput.ts",
	}
	if got := tsNames(tsTree(t, dir)); !slices.Equal(got, want) {
		t.Fatalf("files:\n got %v\nwant %v", got, want)
	}

	if !strings.Contains(tsTree(t, dir)[permissionsTS], "'tickets.ticket:view'") {
		t.Fatal("the application's permission is in the union")
	}
}

func TestTypeScriptLeavesAGoCoreModulesGroupsAndCataloguesOut(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	app := tsApp(dir, []string{tsGenerate}, gocore.WithAuthentication(signedInChain))
	catalogue := tsPermissions()

	// A real module: it extends the application's catalogue with its own permissions and registers it again.
	access.Install(app, catalogue, accessTeam{})
	tsFeature(app, catalogue)

	if len(app.Contracts()) < 2 {
		t.Fatalf("the module's group is installed: %v", app.Contracts())
	}

	if got := len(app.Catalogues()); got != 1 {
		t.Fatalf("one catalogue, however many features hand it over, got %d", got)
	}

	_, err := tsRun(t, app, tsGenerate, "routes")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := tsNames(tsTree(t, dir)), []string{
		"routes/tickets/entities.ts", ticketRoutes, "routes/tickets/schemas.ts",
	}; !slices.Equal(got, want) {
		t.Fatalf("only the application's group is written:\n got %v\nwant %v", got, want)
	}
}

type accessTeam struct{}

func (accessTeam) SubjectNames(context.Context, []authz.Subject) (map[authz.Subject]string, error) {
	return nil, nil //nolint:nilnil // the test installs the module, nobody asks for names
}

func TestTypeScriptRemovesStaleFilesAndReplacesTheRoutesFolder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stale := map[string]string{
		"entities/Old.ts":          "export type Old = {}\n",
		"entities/notes.md":        "kept: not a .ts file\n",
		"schemas/Old.ts":           emptyTS,
		"routes/gone/routes.ts":    emptyTS,
		"routes/gone/sub/other.ts": emptyTS,
		permissionsTS:              "stale\n",
	}

	for name, body := range stale {
		path := filepath.Join(dir, filepath.FromSlash(name))
		writeTS(t, path, body)
	}

	app := tsApp(dir, []string{tsGenerate})
	tsFeature(app, tsPermissions())

	_, err := tsRun(t, app, tsGenerate)
	if err != nil {
		t.Fatal(err)
	}

	tree := tsTree(t, dir)

	gone := []string{"entities/Old.ts", "schemas/Old.ts", "routes/gone/routes.ts", "routes/gone/sub/other.ts"}
	for _, name := range gone {
		if _, ok := tree[name]; ok {
			t.Errorf("%s is stale and must be gone", name)
		}
	}

	if tree["entities/notes.md"] == "" {
		t.Error("Sync leaves what is not a .ts file")
	}

	if tree[permissionsTS] == "stale\n" {
		t.Error("permissions.ts is overwritten")
	}
}

func TestTypeScriptIsByteStableAcrossRuns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	run := func() map[string]string {
		app := tsApp(dir, []string{tsGenerate})
		tsMember(app)
		tsFeature(app, tsPermissions())

		tsMustRun(t, app, tsGenerate)

		return tsTree(t, dir)
	}

	first, second := run(), run()
	if len(first) == 0 || !slices.Equal(tsNames(first), tsNames(second)) {
		t.Fatalf("same files both times: %v / %v", tsNames(first), tsNames(second))
	}

	for name, body := range first {
		if second[name] != body {
			t.Errorf("%s differs between two runs", name)
		}
	}
}

func TestTypeScriptWritesOnePartWhenAskedAndRefusesAnUnknownOne(t *testing.T) {
	t.Parallel()

	for part, want := range map[string]string{
		"entities":    "entities/Label.ts",
		"schemas":     "schemas/tsCloseInput.ts",
		"permissions": permissionsTS,
		"routes":      ticketRoutes,
	} {
		dir := t.TempDir()
		app := tsApp(dir, []string{tsGenerate, part})
		tsFeature(app, tsPermissions())

		_, err := tsRun(t, app, tsGenerate, part)
		if err != nil {
			t.Fatalf("%s: %v", part, err)
		}

		names := tsNames(tsTree(t, dir))
		if !slices.Contains(names, want) || len(names) > 3 {
			t.Errorf("%s: want %s and nothing of the other parts, got %v", part, want, names)
		}
	}

	dir := t.TempDir()
	app := tsApp(dir, []string{tsGenerate, "stuff"})

	_, err := tsRun(t, app, tsGenerate, "stuff")
	if err == nil || !strings.Contains(err.Error(), "entities, schemas, permissions and routes") {
		t.Fatalf("an unknown part names the parts, got %v", err)
	}

	if len(tsTree(t, dir)) != 0 {
		t.Error("nothing is written for an unknown part")
	}
}

func TestTypeScriptNeedsOneCatalogueAndSkipsWhenThereIsNone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	app := tsApp(dir, []string{tsGenerate})
	app.Permissions(tsPermissions())
	app.Permissions(tsPermissions())

	_, err := tsRun(t, app, tsGenerate, "permissions")
	if err == nil || !strings.Contains(err.Error(), "2 permission catalogues") {
		t.Fatalf("two different catalogues are refused with the reason, got %v", err)
	}

	empty := t.TempDir()

	out, err := tsRun(t, tsApp(empty, []string{tsGenerate}), tsGenerate, "permissions")
	if err != nil || len(tsTree(t, empty)) != 0 || !strings.Contains(out, "no permission catalogue") {
		t.Fatalf("no catalogue: nothing written and said so, got %q, %v", out, err)
	}
}

func TestTypeScriptWithNoListsLeavesEntitiesAndSchemasAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keep := filepath.Join(dir, "entities", "Hand.ts")

	writeTS(t, keep, "x\n")

	app := tsBare(tsNoLists(dir)...)
	tsFeature(app, tsPermissions())

	tsMustRun(t, app, tsGenerate)

	_, err := os.Stat(keep)
	if err != nil {
		t.Fatalf("an application that lists no entities keeps its folder: %v", err)
	}
}

//nolint:paralleltest // Chdir cannot be parallel
func TestTypeScriptDefaultFolderIsFrontendGenerated(t *testing.T) {
	t.Chdir(t.TempDir())

	app := tsBare(tsNoLists("")...)
	tsFeature(app, tsPermissions())

	tsMustRun(t, app, tsGenerate)

	for _, want := range []string{"frontend/generated/permissions.ts", "frontend/generated/routes/tickets/routes.ts"} {
		_, err := os.Stat(filepath.FromSlash(want))
		if err != nil {
			t.Errorf("%s: %v", want, err)
		}
	}
}

func TestTypeScriptStopsOnAnInstallProblem(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	app := tsApp(dir, []string{tsGenerate})
	app.Fail("a feature is broken", "fix it")

	_, err := tsRun(t, app, tsGenerate)

	var startup *gocore.StartupError
	if !errors.As(err, &startup) || len(tsTree(t, dir)) != 0 {
		t.Fatalf("a failed Install stops the generator before it writes, got %v", err)
	}
}

func TestNewIsOfflineForTheGeneratorAndNothingElse(t *testing.T) {
	t.Parallel()

	for _, scenario := range []struct {
		args    []string
		offline bool
	}{
		{[]string{tsGenerate}, true},
		{[]string{tsGenerate, "routes"}, true},
		{nil, false},
		{[]string{migrateCmd}, false},
		{[]string{migrateCmd, "status"}, false},
		{[]string{"help"}, false},
		{[]string{pruneName, tsGenerate}, false},
	} {
		app := tsBare(gocore.WithArgs(scenario.args...))

		_, err := tsRun(t, app, "migrate", "status")
		if got := err != nil && strings.Contains(err.Error(), "offline"); got != scenario.offline {
			t.Errorf("args %q: offline is %v, want %v (%v)", scenario.args, got, scenario.offline, err)
		}
	}
}

func TestOfflineFollowsTheOsArgsByDefault(t *testing.T) {
	t.Parallel()

	// go test's own command line asks for no generator.
	app := tsBare()

	_, err := tsRun(t, app, "migrate", "status")
	if err != nil {
		t.Fatalf("an ordinary start is unchanged: %v", err)
	}
}

func TestHelpListsTheGeneratorOnlyWhenTheApplicationUsesIt(t *testing.T) {
	t.Parallel()

	plain := tsBare()
	withTS := tsBare(gocore.WithTypeScript(gocore.TS{Entities: nil, Inputs: nil, Dir: ""}))

	plainHelp, err := tsRun(t, plain, "help")
	if err != nil || strings.Contains(plainHelp, tsGenerate) {
		t.Fatalf("an application without WithTypeScript has the help it had: %q, %v", plainHelp, err)
	}

	help, err := tsRun(t, withTS, "help")
	if err != nil || !strings.Contains(help, tsGenerate) {
		t.Fatalf("help lists the generator: %q, %v", help, err)
	}

	_, err = tsRun(t, tsBare(), tsGenerate)
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("without WithTypeScript it is an unknown command, got %v", err)
	}

	again := tsBare(gocore.WithTypeScript(gocore.TS{Entities: nil, Inputs: nil, Dir: ""}))
	again.Command(tsGenerate, "mine", func(context.Context, *gocore.App, []string, io.Writer) error { return nil })

	err = again.Check()
	if err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Fatalf("the name is go-core's, got %v", err)
	}
}

// Every module that hands routes to the App marks itself with Shipping, or its groups would be generated into the
// application's frontend.
func TestEveryModuleThatMountsRoutesMarksItselfAsShipping(t *testing.T) {
	t.Parallel()

	for _, feature := range []string{"identity", "access", "notification", "savedview", "incident"} {
		if !installMarksShipping(t, feature) {
			t.Errorf("%s.Install must start with `defer app.Shipping()()`", feature)
		}
	}
}

func installMarksShipping(t *testing.T, feature string) bool {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", feature, "*.go"))
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		for _, decl := range parsed.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "Install" {
				return callsShipping(fn)
			}
		}
	}

	return false
}

func callsShipping(fn *ast.FuncDecl) bool {
	found := false

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Shipping" {
			found = true
		}

		return !found
	})

	return found
}
