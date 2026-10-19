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

	"github.com/wssto2/go-core/tenancy"
)

// RunCommand does what args ask, the way Run does with the command line:
//
//	(none)          check, then serve, until ctx is done
//	migrate         apply every pending migration of every connection, then return
//	migrate status  list applied and pending migrations per connection, then return
//	migrate mark-applied <connection> <version|file>...
//	                record migrations as applied without running them, for a database
//	                whose tables already exist, then return
//	ts:generate [--check] [entities|schemas|permissions|routes]
//	                write the application's TypeScript (only with WithTypeScript), then return;
//	                --check writes nothing and fails listing the files that are out of date
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
	if len(args) > 0 && args[0] == tsCommand && a.ts != nil {
		return a.command(func() error { return a.generateTypeScript(args[1:], out) })
	}

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
			_, err := fmt.Fprint(out, usage(a.sortedCommands(), a.ts != nil))

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

func usage(extra []appCommand, typescript bool) string {
	name := binary()

	text := fmt.Sprintf(`Usage: %[1]s [command]

Commands:
  (none)          check the application, then serve it
  migrate         apply every pending migration, then exit
  migrate status  list applied and pending migrations, then exit
  migrate mark-applied <connection> <version|file>...
                  record migrations as applied without running them, then exit
%[2]s  help            show this list
`, name, tsUsage(typescript))

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

// tsUsage is the help lines of ts:generate, empty for an application that does not use it.
func tsUsage(typescript bool) string {
	if !typescript {
		return ""
	}

	return "  " + tsCommand + " [--check] [entities|schemas|permissions|routes]\n" +
		"                  write the application's TypeScript, then exit; --check only lists the files out of date\n"
}

// CommandFunc is a command the application added with App.Command. cmd carries the flags, the App and the output.
type CommandFunc func(ctx context.Context, cmd *Cmd) error

// Cmd is what a command is given: a flag.FlagSet already named after the command, with its output and usage set (so
// -h prints the command's short description and flags), and the App and the writer the command prints to. Declare the
// flags with the FlagSet's methods and List, then call Parse.
type Cmd struct {
	*flag.FlagSet

	// App is the application the command runs in; reach the databases with App.Database.
	App *App
	// Out is where the command writes.
	Out io.Writer

	short, long string
	args        []string
	// positionals is how many arguments Parse accepts after the flags.
	positionals int
}

func newCmd(app *App, c *appCommand, args []string, out io.Writer) *Cmd {
	cmd := new(Cmd)
	cmd.FlagSet = flag.NewFlagSet(c.name, flag.ContinueOnError)
	cmd.App, cmd.Out, cmd.short, cmd.long, cmd.args = app, out, c.short, c.long, args
	cmd.SetOutput(out)
	cmd.Usage = cmd.usage

	return cmd
}

// ParseOption adjusts Cmd.Parse.
type ParseOption func(*Cmd)

// Args allows up to n arguments after the flags, which the command reads with Cmd.Args:
// cmd.Parse(gocore.Args(1)). Without it a stray argument is an error.
func Args(n int) ParseOption { return func(c *Cmd) { c.positionals = n } }

// Parse reads the command line after the command's name into the flags declared so far. "-h" prints the flags and
// ends the command with success (Parse returns flag.ErrHelp, which Run treats as success); an unknown flag, a bad
// value or an argument the command does not allow is an error naming it.
func (c *Cmd) Parse(opts ...ParseOption) error {
	for _, o := range opts {
		o(c)
	}

	err := c.FlagSet.Parse(c.args)
	if err != nil {
		return err //nolint:wrapcheck // flag's own error, which names the flag; ErrHelp must stay recognisable
	}

	if c.NArg() > c.positionals {
		return fmt.Errorf("%w: %s got %q, and takes %s; -h lists the flags",
			errStrayArgument, c.Name(), c.Arg(c.positionals), c.takes())
	}

	return nil
}

// List declares a flag whose value is a comma-separated list: "-only a,b" gives ["a", "b"]. Spaces are trimmed and
// empty items dropped.
func (c *Cmd) List(name, usage string) *[]string {
	items := new([]string)
	c.Var(listFlag{items}, name, usage)

	return items
}

func (c *Cmd) takes() string {
	switch c.positionals {
	case 0:
		return "no arguments, only flags"
	case 1:
		return "one argument"
	default:
		return fmt.Sprintf("at most %d arguments", c.positionals)
	}
}

func (c *Cmd) usage() {
	line := "Usage: " + c.Name() + " [flags]"
	if c.positionals > 0 {
		line += " [arguments]"
	}

	text := line + "\n"
	if c.short != "" {
		text += "\n" + c.short + "\n"
	}

	if c.long != "" {
		text += "\n" + c.long + "\n"
	}

	_, _ = fmt.Fprint(c.Output(), text+"\nFlags:\n")

	c.PrintDefaults()
}

type listFlag struct{ items *[]string }

func (l listFlag) String() string {
	if l.items == nil {
		return ""
	}

	return strings.Join(*l.items, ",")
}

func (l listFlag) Set(v string) error {
	*l.items = nil

	for item := range strings.SplitSeq(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			*l.items = append(*l.items, item)
		}
	}

	return nil
}

type appCommand struct {
	name, short, long string
	run               CommandFunc
}

// CommandOption adjusts App.Command.
type CommandOption func(*appCommand)

// Long gives a command a longer description, printed by "-h" after the usage line and before the flags. The command
// list that "help" prints stays on the short line.
func Long(text string) CommandOption { return func(c *appCommand) { c.long = strings.TrimSpace(text) } }

// isBuiltin is whether name is a command word go-core owns; an application cannot reuse it.
func isBuiltin(name string) bool { return name == "migrate" || name == "help" || name == tsCommand }

// Command adds a command to the application's command line, next to migrate
// and help: "./myapp db:prune --dry-run" runs run with ["--dry-run"]. short is
// the line "help" shows next to the name; gocore.Long adds a longer text for "-h".
//
// ctx is cancelled by Ctrl-C and is marked all-tenants (tenancy.WithAllTenants): a command acts for nobody, so a
// tenant-scoped query sees every tenant; acting for one narrows it with tenancy.WithTenantID.
//
// A command runs the way migrate does: the App is built and every feature
// installed, but nothing is checked, served or booted, no worker or OnStart
// runs, and the databases New opened are closed afterwards. It reaches them
// with app.Database, and may use anything a feature handed out of its
// Install. An App problem (an Install that called Fail) still stops it, with
// the problems listed. The error run returns is Run's error, so the process
// exits non-zero.
//
// The command declares its flags on cmd (a flag.FlagSet with the command's name, usage and output already set, and
// List for comma-separated values) and calls cmd.Parse. "-h" prints the flags and exits 0; an unknown flag or a stray
// argument is an error, unless cmd.Parse(gocore.Args(n)) allows up to n arguments, read with cmd.Args().
//
//	app.Command("db:prune", "delete rows past their retention window", func(ctx context.Context, cmd *gocore.Cmd) error {
//	    dryRun := cmd.Bool("dry-run", false, "count the rows without deleting them")
//	    only := cmd.List("only", "the rules to run, comma-separated")
//	    if err := cmd.Parse(); err != nil {
//	        return err
//	    }
//	    return prune(ctx, cmd.App.Database(), *dryRun, *only, cmd.Out)
//	})
//
// A name may contain ":" (iam:check-placement). Registering a name that is
// empty, has whitespace or starts with "-", is a built-in command (migrate,
// help) or is already registered, or a nil run, is an App problem, reported by
// Check and Run like any other.
func (a *App) Command(name, short string, run CommandFunc, opts ...CommandOption) {
	switch {
	case name == "":
		a.fail("a command was registered with no name", "give app.Command a name such as \"db:prune\"")
	case strings.ContainsFunc(name, unicode.IsSpace):
		a.fail(fmt.Sprintf("command %q contains whitespace", name), "a command is one word, such as \"db:prune\"")
	case strings.HasPrefix(name, "-"):
		a.fail(fmt.Sprintf("command %q starts with \"-\"", name), "that is a flag; name the command without it")
	case isBuiltin(name):
		a.fail(fmt.Sprintf("command %q is a built-in command", name),
			"choose another name; migrate, help and ts:generate belong to go-core")
	case run == nil:
		a.fail(fmt.Sprintf("command %q has no function", name),
			"pass the function to run as the third argument of app.Command")
	default:
		if a.appCommand(name) != nil {
			a.fail(fmt.Sprintf("command %q is registered twice", name), "register each command once, under its own name")

			return
		}

		c := appCommand{name: name, short: short, long: "", run: run}
		for _, o := range opts {
			o(&c)
		}

		a.commands = append(a.commands, c)
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
const commandWords = 5

// commandNames lists every command for an error message: "a, b and c".
func (a *App) commandNames() string {
	sorted := a.sortedCommands()
	names := make([]string, 0, len(sorted)+commandWords)
	names = append(names, "migrate", "migrate status", "migrate mark-applied")

	if a.ts != nil {
		names = append(names, tsCommand)
	}

	names = append(names, "help")

	for _, c := range sorted {
		names = append(names, c.name)
	}

	last := len(names) - 1

	return strings.Join(names[:last], ", ") + " and " + names[last]
}

func (a *App) runAppCommand(ctx context.Context, c *appCommand, args []string, out io.Writer) error {
	if len(a.found()) > 0 {
		return &StartupError{Problems: a.found()}
	}

	// A command acts for nobody: every tenant's rows are in reach, and acting for one narrows with tenancy.WithTenantID.
	err := c.run(tenancy.WithAllTenants(ctx), newCmd(a, c, args, out))
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
	errStrayArgument  = errors.New("unexpected argument")
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
