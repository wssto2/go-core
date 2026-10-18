package gocore

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func portOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
	if err != nil {
		return false
	}

	_ = conn.Close()

	return true
}

const pruneName = "db:prune"

// prune is a command that parses its own flags, the way an application's does.
func prune(_ context.Context, _ *App, args []string, out io.Writer) error {
	fs := flag.NewFlagSet(pruneName, flag.ContinueOnError)
	fs.SetOutput(out)

	dryRun := fs.Bool("dry-run", false, "count the rows without deleting them")
	only := fs.String("only", "", "run only these targets")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("db:prune: %w", err)
	}

	_, err := fmt.Fprintf(out, "dry-run=%t only=%q rest=%v\n", *dryRun, *only, fs.Args())

	return err
}

func TestAppCommandGetsItsArgsUnparsedAndTheDatabase(t *testing.T) {
	app, _ := runnable(t)
	Later[fmt.Stringer](app) // never set: serving would refuse, a command does not care

	var gotApp *App

	app.Command(pruneName, "delete old rows", func(ctx context.Context, a *App, args []string, out io.Writer) error {
		gotApp = a

		if err := a.Database().WithContext(ctx).Exec("select 1").Error; err != nil {
			return err
		}

		return prune(ctx, a, args, out)
	})

	var out strings.Builder
	if err := app.RunCommand(context.Background(), []string{pruneName, "--dry-run", "--only", "a,b", "extra"}, &out); err != nil {
		t.Fatal(err)
	}

	if want := "dry-run=true only=\"a,b\" rest=[extra]\n"; out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}

	if gotApp != app {
		t.Error("the command was not given the App")
	}

	if _, err := app.registry.Get("local"); err == nil {
		t.Error("the database was left open")
	}
}

func TestAppCommandNeverServesNorStartsWorkersOrOnStart(t *testing.T) {
	app, port := runnable(t)
	started := false

	app.OnStart(func(context.Context) error { started = true; return errors.New("must not run") })
	app.Command("noop", "", func(context.Context, *App, []string, io.Writer) error {
		if portOpen(port) {
			t.Error("a command must not open the HTTP port")
		}

		return nil
	})

	if err := app.RunCommand(context.Background(), []string{"noop"}, io.Discard); err != nil {
		t.Fatal(err)
	}

	if started {
		t.Error("OnStart ran for a command")
	}
}

func TestAppCommandHelpFlagPrintsItsFlagsAndSucceeds(t *testing.T) {
	app, _ := runnable(t)
	app.Command(pruneName, "", prune)

	var out strings.Builder
	if err := app.RunCommand(context.Background(), []string{pruneName, "-h"}, &out); err != nil {
		t.Fatalf("-h is not a failure: %v", err)
	}

	for _, want := range []string{"-dry-run", "-only"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("-h output lacks %s: %q", want, out.String())
		}
	}
}

func TestAppCommandErrorFlowsBack(t *testing.T) {
	app, _ := runnable(t)
	boom := errors.New("boom")
	app.Command("fail", "", func(context.Context, *App, []string, io.Writer) error { return boom })

	if err := app.RunCommand(context.Background(), []string{"fail"}, io.Discard); !errors.Is(err, boom) {
		t.Fatalf("want the command's error, got %v", err)
	}

	app, _ = runnable(t)
	app.Command(pruneName, "", prune)

	if err := app.RunCommand(context.Background(), []string{pruneName, "--nope"}, io.Discard); err == nil {
		t.Fatal("a bad flag must fail the command")
	}
}

func TestHelpListsAppCommandsSortedAfterTheBuiltIns(t *testing.T) {
	app, _ := runnable(t)
	app.Command("iam:check-placement", "check each person has one dealer", prune)
	app.Command(pruneName, "delete old rows", prune)

	var out strings.Builder
	if err := app.RunCommand(context.Background(), []string{"help"}, &out); err != nil {
		t.Fatal(err)
	}

	text := out.String()
	order := []string{"migrate mark-applied", "help ", pruneName, "delete old rows", "iam:check-placement", "check each person has one dealer"}

	last := -1
	for _, want := range order {
		at := strings.Index(text, want)
		if at <= last {
			t.Fatalf("%q is missing or out of order in:\n%s", want, text)
		}

		last = at
	}
}

func TestHelpIsUnchangedWithoutAppCommands(t *testing.T) {
	app, _ := runnable(t)

	var out strings.Builder
	if err := app.RunCommand(context.Background(), []string{"help"}, &out); err != nil {
		t.Fatal(err)
	}

	if out.String() != usage(nil) || strings.Contains(out.String(), "Application commands") {
		t.Fatalf("help changed for an app with no commands:\n%s", out.String())
	}
}

func TestUnknownCommandNamesTheAppCommandsToo(t *testing.T) {
	app, _ := runnable(t)
	app.Command(pruneName, "", prune)

	err := app.RunCommand(context.Background(), []string{"frobnicate"}, io.Discard)
	if err == nil || !errors.Is(err, errUnknownCommand) || !strings.Contains(err.Error(), "migrate status") ||
		!strings.Contains(err.Error(), "help and "+pruneName) {
		t.Fatalf("want an error naming the built-in and the app commands, got %v", err)
	}
}

func TestCommandNameProblemsAreStartUpProblems(t *testing.T) {
	run := func(context.Context, *App, []string, io.Writer) error { return nil }

	for _, tc := range []struct {
		name, cmd string
		fn        func(context.Context, *App, []string, io.Writer) error
		want      string
	}{
		{"empty", "", run, "no name"},
		{"built-in migrate", "migrate", run, "built-in"},
		{"built-in help", "help", run, "built-in"},
		{"space", "db prune", run, "whitespace"},
		{"dash", "-x", run, "starts with"},
		{"no function", "nofunc", nil, "no function"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := runnable(t)
			app.Command(tc.cmd, "", tc.fn)

			err := app.Check()

			var startup *StartupError
			if !errors.As(err, &startup) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want a start-up problem containing %q, got %v", tc.want, err)
			}
		})
	}

	t.Run("duplicate", func(t *testing.T) {
		app, _ := runnable(t)
		app.Command("twice", "", run)
		app.Command("twice", "", run)

		if err := app.Check(); err == nil || !strings.Contains(err.Error(), `"twice" is registered twice`) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestAppProblemStopsAnAppCommand(t *testing.T) {
	app, _ := runnable(t)
	ran := false

	app.Command("noop", "", func(context.Context, *App, []string, io.Writer) error { ran = true; return nil })
	app.Fail("a feature could not install", "fix it")

	err := app.RunCommand(context.Background(), []string{"noop"}, io.Discard)

	var startup *StartupError
	if !errors.As(err, &startup) || ran {
		t.Fatalf("want the start-up problems and no run, got ran=%t err=%v", ran, err)
	}

	if _, err := app.registry.Get("local"); err == nil {
		t.Error("the database was left open")
	}
}
