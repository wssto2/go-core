package gocore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/wssto2/go-core/database/migrate"
)

// RunCommand does what args ask, the way Run does with the command line:
//
//	(none)          check, then serve, until ctx is done
//	migrate         apply every pending migration of every connection, then return
//	migrate status  list applied and pending migrations per connection, then return
//	migrate mark-applied <connection> <version|file>...
//	                record migrations as applied without running them, for a database
//	                whose tables already exist, then return
//	help            list these commands, then return
//
// A command other than serving never starts HTTP or boots modules: it uses
// the database connections and closes them. An unknown command returns an
// error naming the commands, so the process exits non-zero. Output goes to out.
//
// Applications do not call it: main is app.Run(). Tests do, with their own
// args and a buffer.
func (a *App) RunCommand(ctx context.Context, args []string, out io.Writer) error {
	command := strings.Join(args, " ")

	if len(args) >= 2 && args[0] == "migrate" && args[1] == "mark-applied" {
		return a.command(func() error { return a.markApplied(ctx, args[2:], out) })
	}

	switch command {
	case "":
		return a.RunContext(ctx)
	case "migrate":
		return a.command(func() error { return a.Migrate(ctx) })
	case "migrate status":
		return a.command(func() error { return a.migrationStatus(ctx, out) })
	case "help", "-h", "--help":
		return a.command(func() error {
			_, err := fmt.Fprint(out, usage())

			return err
		})
	default:
		return a.command(func() error {
			return fmt.Errorf("gocore: unknown command %q: the commands are migrate, migrate status, migrate mark-applied and help", command)
		})
	}
}

// command runs fn as the whole life of the process and closes what New opened.
func (a *App) command(fn func() error) error {
	if a.started {
		return errors.New("gocore: Run was already called on this App")
	}

	a.started = true

	defer a.closeDatabase()

	return fn()
}

func usage() string {
	name := binary()

	return fmt.Sprintf(`Usage: %[1]s [command]

Commands:
  (none)          check the application, then serve it
  migrate         apply every pending migration, then exit
  migrate status  list applied and pending migrations, then exit
  migrate mark-applied <connection> <version|file>...
                  record migrations as applied without running them, then exit
  help            show this list
`, name)
}

func (a *App) migrationStatus(ctx context.Context, out io.Writer) error {
	m, err := a.migrator()
	if err != nil {
		return err
	}

	if m == nil {
		_, err = fmt.Fprintln(out, "no migrations")

		return err
	}

	statuses, err := m.Status(ctx)
	if err != nil {
		return err
	}

	for _, s := range statuses {
		state := "pending"
		if s.Applied {
			state = "applied"
		}

		if _, err := fmt.Fprintf(out, "%-10s %-8s %s\n", s.Connection, state, s.File); err != nil {
			return err
		}
	}

	return nil
}

// markApplied records the versions as applied on the connection (args[0]) without running them.
func (a *App) markApplied(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 2 {
		return errors.New("gocore: usage: migrate mark-applied <connection> <version|file>...")
	}

	versions := make([]int64, len(args)-1)

	for i, arg := range args[1:] {
		v, err := migrate.Version(arg)
		if err != nil {
			return fmt.Errorf("gocore: %w", err)
		}

		versions[i] = v
	}

	m, err := a.migrator()
	if err != nil {
		return err
	}

	if m == nil {
		return errors.New("gocore: mark-applied: the application has no migrations")
	}

	if err := m.MarkApplied(ctx, args[0], versions); err != nil {
		return fmt.Errorf("gocore: %w", err)
	}

	_, err = fmt.Fprintf(out, "%s: %d migration(s) recorded as applied\n", args[0], len(versions))

	return err
}
