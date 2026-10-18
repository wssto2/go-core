package gocore

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
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
//	<name> [args]   a command the application added with Command, then return
//
// A command other than serving never starts HTTP or boots modules: it uses
// the database connections and closes them. An unknown command returns an
// error naming the commands, so the process exits non-zero. Output goes to out.
//
// Applications do not call it: main is app.Run(). Tests do, with their own
// args and a buffer.
func (a *App) RunCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) > 0 {
		if c := a.appCommand(args[0]); c != nil {
			return a.command(func() error { return a.runAppCommand(ctx, c, args[1:], out) })
		}
	}

	command := strings.Join(args, " ")

	if rest, ok := strings.CutPrefix(command+" ", "migrate mark-applied "); ok {
		return a.command(func() error { return a.markApplied(ctx, strings.Fields(rest), out) })
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
			_, err := fmt.Fprint(out, usage(a.sortedCommands()))

			return err
		})
	default:
		return a.command(func() error {
			return fmt.Errorf("gocore: %w %q: the commands are %s", errUnknownCommand, command, a.commandNames())
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

func usage(extra []appCommand) string {
	name := binary()

	text := fmt.Sprintf(`Usage: %[1]s [command]

Commands:
  (none)          check the application, then serve it
  migrate         apply every pending migration, then exit
  migrate status  list applied and pending migrations, then exit
  migrate mark-applied <connection> <version|file>...
                  record migrations as applied without running them, then exit
  help            show this list
`, name)

	if len(extra) == 0 {
		return text
	}

	width := 0
	for _, c := range extra {
		width = max(width, len(c.name))
	}

	var buf strings.Builder

	buf.WriteString(text + "\nApplication commands:\n")

	for _, c := range extra {
		fmt.Fprintf(&buf, "  %-*s  %s\n", width, c.name, c.short)
	}

	return buf.String()
}

// CommandFunc is a command the application added with App.Command. args are
// what follows the command's name on the command line, unparsed; out is where
// the command writes.
type CommandFunc func(ctx context.Context, app *App, args []string, out io.Writer) error

type appCommand struct {
	name, short string
	run         CommandFunc
}

// isBuiltin is whether name is a command word go-core owns; an application cannot reuse it.
func isBuiltin(name string) bool { return name == "migrate" || name == "help" }

// Command adds a command to the application's command line, next to migrate
// and help: "./myapp db:prune --dry-run" runs run with ["--dry-run"]. short is
// the line "help" shows next to the name.
//
// A command runs the way migrate does: the App is built and every feature
// installed, but nothing is checked, served or booted, no worker or OnStart
// runs, and the databases New opened are closed afterwards. It reaches them
// with app.Database, and may use anything a feature handed out of its
// Install. An App problem (an Install that called Fail) still stops it, with
// the problems listed. The error run returns is Run's error, so the process
// exits non-zero.
//
// The arguments after the name are not parsed: the command parses its own,
// usually with the standard flag package. A flag.ErrHelp from run (a FlagSet
// parsed "-h") is a success, so "-h" prints the command's flags and exits 0.
//
//	app.Command("db:prune", "delete rows past their retention window",
//	    func(ctx context.Context, app *gocore.App, args []string, out io.Writer) error {
//	        fs := flag.NewFlagSet("db:prune", flag.ContinueOnError)
//	        fs.SetOutput(out)
//	        dryRun := fs.Bool("dry-run", false, "count the rows without deleting them")
//	        if err := fs.Parse(args); err != nil {
//	            return err
//	        }
//	        return prune(ctx, app.Database(), *dryRun, out)
//	    })
//
// A name may contain ":" (iam:check-placement). Registering a name that is
// empty, has whitespace or starts with "-", is a built-in command (migrate,
// help) or is already registered, or a nil run, is an App problem, reported by
// Check and Run like any other.
func (a *App) Command(name, short string, run CommandFunc) {
	switch {
	case name == "":
		a.fail("a command was registered with no name", "give app.Command a name such as \"db:prune\"")
	case strings.ContainsFunc(name, unicode.IsSpace):
		a.fail(fmt.Sprintf("command %q contains whitespace", name), "a command is one word, such as \"db:prune\"")
	case strings.HasPrefix(name, "-"):
		a.fail(fmt.Sprintf("command %q starts with \"-\"", name), "that is a flag; name the command without it")
	case isBuiltin(name):
		a.fail(fmt.Sprintf("command %q is a built-in command", name),
			"choose another name; migrate and help belong to go-core")
	case run == nil:
		a.fail(fmt.Sprintf("command %q has no function", name),
			"pass the function to run as the third argument of app.Command")
	default:
		if a.appCommand(name) != nil {
			a.fail(fmt.Sprintf("command %q is registered twice", name), "register each command once, under its own name")

			return
		}

		a.commands = append(a.commands, appCommand{name: name, short: short, run: run})
	}
}

func (a *App) appCommand(name string) *appCommand {
	at := slices.IndexFunc(a.commands, func(c appCommand) bool { return c.name == name })
	if at < 0 {
		return nil
	}

	return &a.commands[at]
}

// sortedCommands are the application's commands by name.
func (a *App) sortedCommands() []appCommand {
	return slices.SortedFunc(slices.Values(a.commands), func(x, y appCommand) int { return cmp.Compare(x.name, y.name) })
}

// commandWords is how many built-in commands there are, counting the two-word ones.
const commandWords = 4

// commandNames lists every command for an error message: "a, b and c".
func (a *App) commandNames() string {
	sorted := a.sortedCommands()
	names := make([]string, 0, len(sorted)+commandWords)
	names = append(names, "migrate", "migrate status", "migrate mark-applied", "help")

	for _, c := range sorted {
		names = append(names, c.name)
	}

	last := len(names) - 1

	return strings.Join(names[:last], ", ") + " and " + names[last]
}

func (a *App) runAppCommand(ctx context.Context, c *appCommand, args []string, out io.Writer) error {
	if len(a.problems) > 0 {
		return &StartupError{Problems: append([]Problem(nil), a.problems...)}
	}

	err := c.run(ctx, a, args, out)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}

	return err
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

var (
	errUnknownCommand = errors.New("unknown command")
	errMarkUsage      = errors.New("gocore: mark-applied takes a connection and at least one version or file")
)

// markApplied is the mark-applied command: args are a connection and the versions or files.
func (a *App) markApplied(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < minMarkArgs {
		return errMarkUsage
	}

	err := a.MarkApplied(ctx, args[0], args[1:]...)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(out, "%s: %d migration(s) recorded as applied\n", args[0], len(args)-1)
	if err != nil {
		return fmt.Errorf("gocore: writing the result: %w", err)
	}

	return nil
}

// minMarkArgs is a connection and one version.
const minMarkArgs = 2
