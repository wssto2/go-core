package gocore

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wssto2/go-core/authz/authzts"
	"github.com/wssto2/go-core/contract"
	"github.com/wssto2/go-core/go2ts"
)

// tsCommand is the built-in command that writes the application's TypeScript.
const tsCommand = "ts:generate"

// defaultTSDir is where ts:generate writes when TS.Dir is empty.
const defaultTSDir = "frontend/generated"

// checkFlag makes ts:generate list the drifted files instead of writing.
const checkFlag = "--check"

var (
	errUnknownPart    = errors.New("unknown part")
	errTSDrift        = errors.New("generated files are out of date")
	errManyCatalogues = errors.New(
		"the Permission union is written from one: define the permissions of the others in the application's catalogue")
)

// TS is what an application hands to WithTypeScript: the lists of Go types that are written as TypeScript beside the
// route tables and the permission union, which ts:generate derives from the installed application.
type TS struct {
	// Entities are the response types the frontend imports from entities/: go2ts.GenerateTypes' argument, so
	// go2ts.As renames, go2ts.Enum entries and nested structs work as there. Empty: entities/ is left as it is.
	Entities []any
	// Inputs are the input types that get a Zod schema in schemas/ (go2ts.GenerateSchemas' argument), beside the
	// ones the route groups name, which are written with their group. Empty: schemas/ is left as it is.
	Inputs []any
	// Dir is the folder everything is written under, relative to where the command runs; empty is
	// "frontend/generated". The layout below it is fixed: entities/, schemas/, routes/<group>/ and permissions.ts.
	Dir string
}

// WithTypeScript adds the built-in command ts:generate, which writes the application's TypeScript from the installed
// application and the lists in ts:
//
//	routes/<group>/{routes,entities,schemas}.ts   one folder per route group (route.Group) of the application's own
//	                                              features, not go-core's modules' (see Shipping); the folder is
//	                                              removed first, so a removed group leaves nothing behind
//	entities/                                     ts.Entities, through go2ts.Sync: files no longer generated are deleted
//	schemas/                                      ts.Inputs, through go2ts.Sync
//	permissions.ts                                the permission union and its metadata, from App.Catalogues, overwritten
//
// "./myapp ts:generate" writes all four; "./myapp ts:generate routes" (or entities, schemas, permissions) one.
// "./myapp ts:generate --check" (with or without a part) writes nothing: it lists the generated files that differ
// from what the application would write, missing, edited and stale ones, and exits non-zero when there are any, so a
// CI step is that one command. It runs
// offline: New sees the command line and makes the App Offline itself, so main is unchanged and needs no database,
// but the Installs still run and need the settings they read (see Offline). Output is deterministic, so a regenerate
// and diff check in CI is stable.
//
// Exactly one permission catalogue is written. Handing the same one to Permissions several times (as access.Install
// does with the application's) counts once; two different catalogues stop the command, because one Permission union
// cannot say which of two definitions of the same identifier wins: define the second's permissions in the first. An
// application with no catalogue gets no permissions.ts.
//
// The help command lists ts:generate only when the application used this option; "ts:generate" is not a name for
// Command.
func WithTypeScript(ts TS) Option {
	return func(app *App) { app.ts = &ts }
}

// WithArgs sets the command line the App runs, instead of os.Args[1:]: New reads it to decide whether the command
// is one that runs offline (ts:generate), and Run executes it. For tests, which have no command line of their own:
//
//	app := gocore.New(gocore.WithArgs("ts:generate"), gocore.WithTypeScript(ts))
//
// No arguments means serving.
func WithArgs(args ...string) Option {
	return func(app *App) { app.args, app.argsSet = args, true }
}

// commandLine is what the process was asked to do: WithArgs, else os.Args[1:].
func (a *App) commandLine() []string {
	switch {
	case a.argsSet:
		return a.args
	case len(os.Args) > 1:
		return os.Args[1:]
	default:
		return nil
	}
}

// asksForOffline is whether the command line starts with a command that needs the installed application and nothing
// outside it, so New makes the App offline: today only ts:generate.
func asksForOffline(args []string) bool {
	return len(args) > 0 && args[0] == tsCommand
}

// tsParts are the parts ts:generate writes, in the order it writes them.
func tsParts() []string { return []string{"entities", "schemas", "permissions", "routes"} }

func (a *App) generateTypeScript(args []string, out io.Writer) error {
	if len(a.found()) > 0 {
		return &StartupError{Problems: a.found()}
	}

	check := slices.Contains(args, checkFlag)
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == checkFlag })
	parts := tsParts()

	if len(args) > 0 {
		if len(args) > 1 || !slices.Contains(parts, args[0]) {
			return fmt.Errorf("gocore: ts:generate: %w %q: the parts are %s and %s", errUnknownPart,
				strings.Join(args, " "), strings.Join(parts[:len(parts)-1], ", "), parts[len(parts)-1])
		}

		parts = args
	}

	dir := cmp.Or(a.ts.Dir, defaultTSDir)

	if check {
		return a.checkTypeScript(parts, dir, out)
	}

	for _, part := range parts {
		err := a.generatePart(part, dir, out)
		if err != nil {
			return err
		}
	}

	return nil
}

func (a *App) generatePart(part, dir string, out io.Writer) error {
	target := filepath.Join(dir, part)

	var err error

	switch part {
	case "entities":
		err = a.syncTypes(target, a.ts.Entities, out, go2ts.GenerateTypes)
	case "schemas":
		err = a.syncTypes(target, a.ts.Inputs, out, go2ts.GenerateSchemas)
	case "permissions":
		err = a.generatePermissions(target+".ts", out)
	default:
		err = a.generateRoutes(target, out)
	}

	if err != nil {
		return fmt.Errorf("gocore: ts:generate %s: %w", part, err)
	}

	return nil
}

// syncTypes writes list into dir with generate, through Sync. An empty list is "this application has none": the folder
// is not touched, where Sync would empty it.
func (a *App) syncTypes(dir string, list []any, out io.Writer, generate func([]any, string) error) error {
	if len(list) == 0 {
		_, err := fmt.Fprintf(out, "%s: no types listed, left as it is\n", dir)

		return err //nolint:wrapcheck // the writer's own error
	}

	err := go2ts.Sync(dir, func(scratch string) error { return generate(list, scratch) })
	if err != nil {
		return err //nolint:wrapcheck // go2ts names the folder and the file
	}

	_, err = fmt.Fprintf(out, "%s: %d types listed, written\n", dir, len(list))

	return err //nolint:wrapcheck // the writer's own error
}

func (a *App) generatePermissions(file string, out io.Writer) error {
	catalogues := a.Catalogues()

	switch len(catalogues) {
	case 0:
		_, err := fmt.Fprintf(out, "%s: no permission catalogue installed, nothing written\n", file)

		return err //nolint:wrapcheck // the writer's own error
	case 1:
		err := authzts.WriteFile(catalogues[0], file, authzts.Options{Hierarchy: nil})
		if err != nil {
			return err //nolint:wrapcheck // authzts names the file
		}

		_, err = fmt.Fprintf(out, "%s: %d permissions written\n", file, catalogues[0].Len())

		return err //nolint:wrapcheck // the writer's own error
	default:
		return fmt.Errorf("%d permission catalogues were installed: %w", len(catalogues), errManyCatalogues)
	}
}

// generateRoutes writes the route tables of the application's own groups. The folder holds only generated route
// tables, so it is removed first and a removed group leaves nothing behind.
func (a *App) generateRoutes(dir string, out io.Writer) error {
	own := a.OwnContracts()

	err := os.RemoveAll(dir)
	if err != nil {
		return fmt.Errorf("clear %s: %w", dir, err)
	}

	err = contract.Generate(dir, own...)
	if err != nil {
		return err //nolint:wrapcheck // contract lists every problem with its fix
	}

	_, err = fmt.Fprintf(out, "%s: %d route groups written\n", dir, len(own))

	return err //nolint:wrapcheck // the writer's own error
}

// checkTypeScript lists, writing nothing, the files of the parts that ts:generate would write or delete, and fails
// when there are any.
func (a *App) checkTypeScript(parts []string, dir string, out io.Writer) error {
	var drifted []string

	for _, part := range parts {
		found, err := a.driftOf(part, filepath.Join(dir, part))
		if err != nil {
			return fmt.Errorf("gocore: ts:generate --check %s: %w", part, err)
		}

		drifted = append(drifted, found...)
	}

	if len(drifted) == 0 {
		_, err := fmt.Fprintf(out, "%s: generated files are up to date\n", dir)

		return err //nolint:wrapcheck // the writer's own error
	}

	for _, file := range drifted {
		_, err := fmt.Fprintln(out, file)
		if err != nil {
			return err //nolint:wrapcheck // the writer's own error
		}
	}

	return fmt.Errorf("gocore: ts:generate --check: %d %w: run %s to write them", len(drifted), errTSDrift, tsCommand)
}

// driftOf is the paths of one part that differ from what generating would write.
func (a *App) driftOf(part, target string) ([]string, error) {
	switch part {
	case "entities":
		return driftTypes(target, a.ts.Entities, go2ts.GenerateTypes)
	case "schemas":
		return driftTypes(target, a.ts.Inputs, go2ts.GenerateSchemas)
	case "permissions":
		return a.driftPermissions(target + ".ts")
	default:
		return a.driftRoutes(target)
	}
}

func driftTypes(dir string, list []any, generate func([]any, string) error) ([]string, error) {
	if len(list) == 0 {
		return []string{}, nil // "no types listed": the folder is left as it is
	}

	names, err := go2ts.Drift(dir, func(scratch string) error { return generate(list, scratch) })

	return pathsIn(dir, names), err
}

func (a *App) driftPermissions(file string) ([]string, error) {
	catalogues := a.Catalogues()

	switch len(catalogues) {
	case 0:
		return []string{}, nil
	case 1:
		scratch, err := os.MkdirTemp("", "gocore-ts-")
		if err != nil {
			return nil, fmt.Errorf("create the scratch folder: %w", err)
		}

		defer func() { _ = os.RemoveAll(scratch) }() // best-effort: a scratch folder of the temp dir

		want := filepath.Join(scratch, filepath.Base(file))

		err = authzts.WriteFile(catalogues[0], want, authzts.Options{Hierarchy: nil})
		if err != nil {
			return nil, err //nolint:wrapcheck // authzts names the file
		}

		if sameFile(want, file) {
			return []string{}, nil
		}

		return []string{file}, nil
	default:
		return nil, fmt.Errorf("%d permission catalogues were installed: %w", len(catalogues), errManyCatalogues)
	}
}

func (a *App) driftRoutes(dir string) ([]string, error) {
	scratch, err := os.MkdirTemp("", "gocore-ts-")
	if err != nil {
		return nil, fmt.Errorf("create the scratch folder: %w", err)
	}

	defer func() { _ = os.RemoveAll(scratch) }() // best-effort: a scratch folder of the temp dir

	err = contract.Generate(scratch, a.OwnContracts()...)
	if err != nil {
		return nil, err //nolint:wrapcheck // contract lists every problem with its fix
	}

	return treeDrift(scratch, dir)
}

// sameFile reports whether the two files hold the same bytes; a file that cannot be read is not the same.
func sameFile(want, have string) bool {
	generated, err := os.ReadFile(want) //nolint:gosec // our own scratch directory
	if err != nil {
		return false
	}

	current, err := os.ReadFile(have) //nolint:gosec // the file the application named
	if err != nil {
		return false
	}

	return bytes.Equal(current, generated)
}

// filesUnder lists the files below dir, relative; a dir that does not exist holds none.
func filesUnder(dir string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return err
		case !entry.IsDir():
			rel, _ := filepath.Rel(dir, path)
			files = append(files, rel)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}

	return files, nil
}

// treeDrift lists the files under have that differ from the generated folder want, or want holds and have lacks,
// or have holds and want does not.
func treeDrift(want, have string) ([]string, error) {
	generated, err := filesUnder(want)
	if err != nil {
		return nil, err
	}

	current, err := filesUnder(have)
	if err != nil {
		return nil, err
	}

	var drifted []string

	for _, rel := range generated {
		if !sameFile(filepath.Join(want, rel), filepath.Join(have, rel)) {
			drifted = append(drifted, filepath.Join(have, rel))
		}
	}

	for _, rel := range current {
		if !slices.Contains(generated, rel) {
			drifted = append(drifted, filepath.Join(have, rel))
		}
	}

	slices.Sort(drifted)

	return drifted, nil
}

func pathsIn(dir string, names []string) []string {
	out := make([]string, len(names))
	for idx, name := range names {
		out[idx] = filepath.Join(dir, name)
	}

	return out
}
