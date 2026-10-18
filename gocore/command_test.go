package gocore_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
)

const (
	pruneName = "db:prune"
	helpCmd   = "help"
)

var errBoom = errors.New("boom")

// prune is a command that parses its own flags, the way an application's does.
func prune(_ context.Context, _ *gocore.App, args []string, out io.Writer) error {
	flags := flag.NewFlagSet(pruneName, flag.ContinueOnError)
	flags.SetOutput(out)

	dryRun := flags.Bool("dry-run", false, "count the rows without deleting them")
	only := flags.String("only", "", "run only these targets")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("db:prune: %w", err)
	}

	_, err = fmt.Fprintf(out, "dry-run=%t only=%q rest=%v\n", *dryRun, *only, flags.Args())
	if err != nil {
		return fmt.Errorf("db:prune: %w", err)
	}

	return nil
}

func nothing(context.Context, *gocore.App, []string, io.Writer) error { return nil }

// commandApp is an App over a test database, and the registry to see whether it was closed.
func commandApp(t *testing.T) (*gocore.App, *database.Registry) {
	t.Helper()

	reg, cleanup := database.NewTestRegistry(localConn)

	t.Cleanup(func() { _ = cleanup() })

	return newApp(reg), reg
}

func runArgs(app *gocore.App, args ...string) (string, error) {
	var out strings.Builder

	err := app.RunCommand(context.Background(), args, &out)

	return out.String(), err
}

func TestAppCommandGetsItsArgsUnparsedAndTheDatabase(t *testing.T) {
	t.Parallel()

	app, reg := commandApp(t)
	gocore.Later[fmt.Stringer](app) // never set: serving would refuse, a command does not care

	var got *gocore.App

	app.Command(pruneName, "delete old rows", func(
		ctx context.Context, given *gocore.App, args []string, out io.Writer,
	) error {
		got = given

		err := given.Database().WithContext(ctx).Exec("select 1").Error
		if err != nil {
			return fmt.Errorf("the command's database: %w", err)
		}

		return prune(ctx, given, args, out)
	})

	out, err := runArgs(app, pruneName, "--dry-run", "--only", "a,b", "extra")
	if err != nil {
		t.Fatal(err)
	}

	if want := "dry-run=true only=\"a,b\" rest=[extra]\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}

	if got != app {
		t.Error("the command was not given the App")
	}

	_, err = reg.Get(localConn)
	if err == nil {
		t.Error("the database was left open")
	}
}

func TestAppCommandDoesNotRunOnStartChecks(t *testing.T) {
	t.Parallel()

	app, _ := commandApp(t)
	started := false

	app.OnStart(func(context.Context) error {
		started = true

		return errBoom
	})
	app.Command("noop", "", nothing)

	_, err := runArgs(app, "noop")
	if err != nil {
		t.Fatal(err)
	}

	if started {
		t.Error("OnStart ran for a command")
	}
}

func TestAppCommandHelpFlagPrintsItsFlagsAndSucceeds(t *testing.T) {
	t.Parallel()

	app, _ := commandApp(t)
	app.Command(pruneName, "", prune)

	out, err := runArgs(app, pruneName, "-h")
	if err != nil {
		t.Fatalf("-h is not a failure: %v", err)
	}

	for _, want := range []string{"-dry-run", "-only"} {
		if !strings.Contains(out, want) {
			t.Errorf("-h output lacks %s: %q", want, out)
		}
	}
}

func TestAppCommandErrorFlowsBack(t *testing.T) {
	t.Parallel()

	app, _ := commandApp(t)
	app.Command("fail", "", func(context.Context, *gocore.App, []string, io.Writer) error { return errBoom })

	_, err := runArgs(app, "fail")
	if !errors.Is(err, errBoom) {
		t.Fatalf("want the command's error, got %v", err)
	}

	app, _ = commandApp(t)
	app.Command(pruneName, "", prune)

	_, err = runArgs(app, pruneName, "--nope")
	if err == nil {
		t.Fatal("a bad flag must fail the command")
	}
}

func TestHelpListsAppCommandsSortedAfterTheBuiltIns(t *testing.T) {
	t.Parallel()

	app, _ := commandApp(t)
	app.Command("iam:check-placement", "check each person has one dealer", prune)
	app.Command(pruneName, "delete old rows", prune)

	text, err := runArgs(app, helpCmd)
	if err != nil {
		t.Fatal(err)
	}

	last := -1

	for _, want := range []string{
		"migrate mark-applied", "help ", pruneName, "delete old rows",
		"iam:check-placement", "check each person has one dealer",
	} {
		at := strings.Index(text, want)
		if at <= last {
			t.Fatalf("%q is missing or out of order in:\n%s", want, text)
		}

		last = at
	}
}

func TestHelpIsUnchangedWithoutAppCommands(t *testing.T) {
	t.Parallel()

	app, _ := commandApp(t)

	text, err := runArgs(app, helpCmd)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(text, "Application commands") || !strings.HasSuffix(text, "  help            show this list\n") {
		t.Fatalf("help changed for an app with no commands:\n%s", text)
	}
}

func TestUnknownCommandNamesTheAppCommandsToo(t *testing.T) {
	t.Parallel()

	app, _ := commandApp(t)
	app.Command(pruneName, "", prune)

	_, err := runArgs(app, "frobnicate")
	if err == nil || !strings.Contains(err.Error(), "migrate status") ||
		!strings.Contains(err.Error(), "help and "+pruneName) {
		t.Fatalf("want an error naming the built-in and the app commands, got %v", err)
	}
}

func TestCommandNameProblemsAreStartUpProblems(t *testing.T) {
	t.Parallel()

	for _, scenario := range []struct {
		name, cmd string
		run       gocore.CommandFunc
		want      string
	}{
		{"empty", "", nothing, "no name"},
		{"built-in migrate", migrateCmd, nothing, "built-in"},
		{"built-in help", helpCmd, nothing, "built-in"},
		{"space", "db prune", nothing, "whitespace"},
		{"dash", "-x", nothing, "starts with"},
		{"no function", "nofunc", nil, "no function"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			app, _ := commandApp(t)
			app.Command(scenario.cmd, "", scenario.run)

			err := app.Check()

			var startup *gocore.StartupError
			if !errors.As(err, &startup) || !strings.Contains(err.Error(), scenario.want) {
				t.Fatalf("want a start-up problem containing %q, got %v", scenario.want, err)
			}
		})
	}

	t.Run("duplicate", func(t *testing.T) {
		t.Parallel()

		app, _ := commandApp(t)
		app.Command("twice", "", nothing)
		app.Command("twice", "", nothing)

		err := app.Check()
		if err == nil || !strings.Contains(err.Error(), `"twice" is registered twice`) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestAppProblemStopsAnAppCommand(t *testing.T) {
	t.Parallel()

	app, reg := commandApp(t)
	ran := false

	app.Command("noop", "", func(context.Context, *gocore.App, []string, io.Writer) error {
		ran = true

		return nil
	})
	app.Fail("a feature could not install", "fix it")

	_, err := runArgs(app, "noop")

	var startup *gocore.StartupError
	if !errors.As(err, &startup) || ran {
		t.Fatalf("want the start-up problems and no run, got ran=%t err=%v", ran, err)
	}

	_, err = reg.Get(localConn)
	if err == nil {
		t.Error("the database was left open")
	}
}
