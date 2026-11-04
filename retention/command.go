package retention

import (
	"context"
	"fmt"
	"io"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
)

// Command registers db:prune on app: db:prune [-dry-run] [-only a,b] applies rules to the database registered as
// conn and prints what each rule matched, deleted or redacted. rules is called when the command runs, so the windows
// are read then; short is the line `help` lists, help the long text of -h (an empty one leaves just the flags).
//
//	retention.Command(app, "local", func() []retention.Rule { return pruneRules },
//		"delete rows past their retention window", "Delete rows past their window.")
//
// Ctrl-C or SIGTERM stops between batches; what was pruned stays pruned.
func Command(app *gocore.App, conn database.Connection, rules func() []Rule, short, help string) {
	run := func(ctx context.Context, cmd *gocore.Cmd) error {
		dryRun := cmd.Bool("dry-run", false, "Count the rows past each window without changing them")
		only := cmd.List("only", "Run only these targets (comma-separated)")

		err := cmd.Parse()
		if err != nil {
			return fmt.Errorf("db:prune: %w", err)
		}

		pruner, err := New(cmd.App.Database(conn), cmd.App.Clock(), rules()...)
		if err != nil {
			return fmt.Errorf("prune rules: %w", err)
		}

		report, runErr := pruner.Run(ctx, Options{DryRun: *dryRun, Only: *only})

		printReport(cmd.Out, report)

		if runErr != nil {
			return fmt.Errorf("prune: %w", runErr)
		}

		return nil
	}

	app.Command("db:prune", short, run, gocore.Long(help))
}

// hoursPerDay turns a window into the days the report shows it in.
const hoursPerDay = 24

func printReport(out io.Writer, report Report) {
	if report.DryRun {
		_, _ = fmt.Fprintln(out, "Dry run — nothing is changed.")
	}

	for _, result := range report.Results {
		days, cutoff := int(result.Rule.Keep.Hours()/hoursPerDay), result.Cutoff.Format("2006-01-02")
		label := fmt.Sprintf("%-24s keep %3dd  before %s", result.Rule.Name, days, cutoff)
		done, pending := "deleted", "to delete"

		if result.Rule.Set != nil {
			done, pending = "redacted", "to redact"
		}

		switch {
		case result.Err != nil:
			_, _ = fmt.Fprintf(out, "✕ %s  %s\n", label, result.Err)
		case report.DryRun:
			_, _ = fmt.Fprintf(out, "· %s  %d %s\n", label, result.Matched, pending)
		default:
			_, _ = fmt.Fprintf(out, "✓ %s  %d %s\n", label, result.Affected, done)
		}
	}
}
