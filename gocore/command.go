package gocore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// RunCommand does what args ask, the way Run does with the command line:
//
//	(none)          check, then serve, until ctx is done
//	migrate         apply every pending migration of every connection, then return
//	migrate status  list applied and pending migrations per connection, then return
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
			return fmt.Errorf("gocore: unknown command %q: the commands are migrate, migrate status and help", command)
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
