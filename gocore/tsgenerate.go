package gocore

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wssto2/go-core/authz/authzts"
	"github.com/wssto2/go-core/contract"
	"github.com/wssto2/go-core/go2ts"
	"github.com/wssto2/go-core/route"
)

// tsCommand is the built-in command that writes the application's TypeScript.
const tsCommand = "ts:generate"

// defaultTSDir is where ts:generate writes when TS.Dir is empty.
const defaultTSDir = "frontend/generated"

var (
	errUnknownPart    = errors.New("unknown part")
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
// "./myapp ts:generate" writes all four; "./myapp ts:generate routes" (or entities, schemas, permissions) one. It runs
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
//	app := gocore.New(cfg, gocore.WithArgs("ts:generate"), gocore.WithTypeScript(ts))
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
	if len(a.problems) > 0 {
		return &StartupError{Problems: append([]Problem(nil), a.problems...)}
	}

	parts := tsParts()

	if len(args) > 0 {
		if len(args) > 1 || !slices.Contains(parts, args[0]) {
			return fmt.Errorf("gocore: ts:generate: %w %q: the parts are %s and %s", errUnknownPart,
				strings.Join(args, " "), strings.Join(parts[:len(parts)-1], ", "), parts[len(parts)-1])
		}

		parts = args
	}

	dir := cmp.Or(a.ts.Dir, defaultTSDir)

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
	var own []*route.Contract

	for _, c := range a.Contracts() {
		if !a.shipped[c] {
			own = append(own, c)
		}
	}

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
