// Package retention deletes or redacts the rows of tables an application owns
// once they are older than the application decides to keep them: expired
// sessions, one-time codes, finished job rows, the raw payload of a finished
// import.
//
// Use it for that housekeeping of an application's own tables, run from a
// command on a schedule (a cron entry, a CLI command) so a long delete never
// competes with requests. The modules go-core ships keep their own tables clean
// with their own housekeepers; this package is for the rest.
//
// The application owns its rules. go-core ships no rule and does not know any
// table: each Rule names a table, the SQL that selects the rows past their
// window, and how long to keep them. Rules are trusted code, written by the
// application's developers, never built from input.
//
//	pruner, err := retention.New(db, nil,
//		retention.Rule{Name: "sessions", Table: "tokens", Keep: 7 * 24 * time.Hour,
//			Where: "expires_at < @cutoff"},
//		retention.Rule{Name: "payloads", Table: "import_items", Keep: 90 * 24 * time.Hour,
//			Set:   map[string]any{"raw_data": "[]"},
//			Where: "raw_data <> '[]' AND processed_at < @cutoff AND " +
//				"import_id IN (SELECT id FROM imports WHERE status IN @done)",
//			Args:  map[string]any{"done": []int{3, 4}}},
//	)
//	if err != nil {
//		return err
//	}
//
//	report, err := pruner.Run(ctx, retention.Options{DryRun: true})
//
// A run works in batches of 500 rows, each statement committing on its own, so
// no statement holds locks for long and an interrupted run loses nothing: what
// was pruned stays pruned and the next run goes on. It targets MySQL 8 and later
// and MariaDB 10.3 and later, and uses nothing either lacks: only single-table
// DELETE ... ORDER BY ... LIMIT and UPDATE ... ORDER BY ... LIMIT. The statements
// use the database as given, so a transaction in db is the caller's choice; pass
// the plain connection to keep the batches independent.
package retention

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
)

// batchSize bounds each DELETE and UPDATE.
const batchSize = 500

// Parameters of the statements that a rule cannot use for its own: @cutoff is
// the moment before which a row is past its window, @limit the batch size, and
// names starting with paramPrefix hold the values of Set.
const (
	paramCutoff = "cutoff"
	paramLimit  = "limit"
	paramPrefix = "retention_"
)

var (
	identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	ruleName   = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
)

// The errors New and Run wrap.
var (
	// ErrInvalidRule is wrapped by New's error for a rule that cannot run as written.
	ErrInvalidRule = errors.New("invalid rule")
	// ErrUnknownRule is wrapped by Run's error for a name in Options.Only that no rule has.
	ErrUnknownRule = errors.New("unknown rule")
	// ErrNoDatabase is returned by New when it was given no database.
	ErrNoDatabase = errors.New("no database")
)

// Clock tells the time.
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Rule is one retention rule: the rows of Table that Where selects, and that are
// past Keep, are deleted, or redacted when Set is given.
type Rule struct {
	// Name identifies the rule in reports and in Options.Only: lowercase letters,
	// digits, '_', '.' and '-', starting with a letter. Names are unique.
	Name string
	// Table is a plain table name of the connection's schema.
	Table string
	// Where is the SQL that selects the rows past their window. It must use the
	// named parameter @cutoff, which is the run's "now" minus Keep, and may use
	// the parameters in Args: name them as GORM does (@name; a slice expands for
	// IN @name). Subqueries must read other tables: MySQL and MariaDB refuse to
	// change a table that the same statement reads. After a redaction the rows
	// must no longer match Where, or the next run touches them again.
	Where string
	// Set, when not nil, redacts the matching rows instead of deleting them:
	// column name to the value it is set to (nil is NULL).
	Set map[string]any
	// OrderBy is the plain column the batches go in; "id" when empty.
	OrderBy string
	// Keep is how long a row is kept, counted from the time Where compares with
	// @cutoff. It must be positive.
	Keep time.Duration
	// Args are the named parameters Where uses besides @cutoff. The names cutoff,
	// limit and retention_* are taken.
	Args map[string]any
}

// Options selects what Run does.
type Options struct {
	// DryRun counts the rows past their window without changing them.
	DryRun bool
	// Only limits the run to the rules with these names; empty means every rule.
	Only []string
}

// Result is one rule's outcome.
type Result struct {
	Rule Rule
	// Cutoff is the moment before which a row was past the rule's window.
	Cutoff time.Time
	// Matched is how many rows were past the window when the rule started.
	Matched int64
	// Affected is how many rows were deleted or redacted; zero on a dry run.
	Affected int64
	// Err is why the rule stopped, if it did. Matched and Affected are what it
	// had done by then.
	Err error
}

// Report is the outcome of one Run, one Result per rule that ran, in the order
// the rules were given to New.
type Report struct {
	DryRun  bool
	Results []Result
}

// Pruner applies a fixed set of rules. It is safe for concurrent use.
type Pruner struct {
	conn  *gorm.DB
	clock Clock
	rules []prepared
}

// prepared is a rule with its two statements built.
type prepared struct {
	rule  Rule
	count string
	apply string
}

// New returns a Pruner for the rules, which run in the order given. It checks
// every rule and returns all the problems at once, each wrapping ErrInvalidRule:
// an identifier that is not plain, a missing @cutoff, a Keep that is not
// positive, a name used twice. A nil clock is the system clock.
func New(conn *gorm.DB, clock Clock, rules ...Rule) (*Pruner, error) {
	if conn == nil {
		return nil, fmt.Errorf("retention: %w", ErrNoDatabase)
	}

	if clock == nil {
		clock = systemClock{}
	}

	var errs []error

	seen := make(map[string]bool, len(rules))
	built := make([]prepared, 0, len(rules))

	for _, rule := range rules {
		if seen[rule.Name] {
			errs = append(errs, fmt.Errorf("retention: %w: %q is defined twice", ErrInvalidRule, rule.Name))

			continue
		}

		seen[rule.Name] = true

		prep, err := prepare(rule)
		if err != nil {
			errs = append(errs, fmt.Errorf("retention: rule %q: %w", rule.Name, err))

			continue
		}

		built = append(built, prep)
	}

	err := errors.Join(errs...)
	if err != nil {
		return nil, err
	}

	return &Pruner{conn: conn, clock: clock, rules: built}, nil
}

func prepare(rule Rule) (prepared, error) {
	if rule.OrderBy == "" {
		rule.OrderBy = "id"
	}

	err := check(rule)
	if err != nil {
		return prepared{}, err
	}

	set, err := setClause(rule.Set)
	if err != nil {
		return prepared{}, err
	}

	where := " WHERE (" + rule.Where + ")"
	table := "`" + rule.Table + "`"
	tail := where + " ORDER BY `" + rule.OrderBy + "` LIMIT @" + paramLimit
	apply := "DELETE FROM " + table + tail

	if rule.Set != nil {
		apply = "UPDATE " + table + " SET " + set + tail
	}

	return prepared{rule: rule, count: "SELECT COUNT(*) FROM " + table + where, apply: apply}, nil
}

func check(rule Rule) error {
	switch {
	case !ruleName.MatchString(rule.Name):
		return fmt.Errorf("%w: the name must be lowercase letters, digits, '_', '.' or '-', starting with a letter",
			ErrInvalidRule)
	case !identifier.MatchString(rule.Table):
		return fmt.Errorf("%w: the table %q is not a plain identifier", ErrInvalidRule, rule.Table)
	case !identifier.MatchString(rule.OrderBy):
		return fmt.Errorf("%w: the order column %q is not a plain identifier", ErrInvalidRule, rule.OrderBy)
	case rule.Keep <= 0:
		return fmt.Errorf("%w: the Keep must be positive", ErrInvalidRule)
	case !strings.Contains(rule.Where, "@"+paramCutoff):
		return fmt.Errorf("%w: Where must select by @cutoff, or Keep means nothing", ErrInvalidRule)
	}

	for name := range rule.Args {
		if name == paramCutoff || name == paramLimit || strings.HasPrefix(name, paramPrefix) {
			return fmt.Errorf("%w: the argument name %q is reserved", ErrInvalidRule, name)
		}
	}

	return nil
}

// setClause is "`a` = @retention_0, `b` = @retention_1" with the columns in
// name order, or "" for a rule that deletes.
func setClause(set map[string]any) (string, error) {
	if set != nil && len(set) == 0 {
		return "", fmt.Errorf("%w: an empty Set: leave it nil to delete", ErrInvalidRule)
	}

	cols := slices.Sorted(maps.Keys(set))
	parts := make([]string, len(cols))

	for i, col := range cols {
		if !identifier.MatchString(col) {
			return "", fmt.Errorf("%w: the Set column %q is not a plain identifier", ErrInvalidRule, col)
		}

		parts[i] = fmt.Sprintf("`%s` = @%s%d", col, paramPrefix, i)
	}

	return strings.Join(parts, ", "), nil
}

// params are the named parameters of a statement of prep.
func (prep prepared) params(cutoff time.Time, limit int) map[string]any {
	params := maps.Clone(prep.rule.Args)
	if params == nil {
		params = map[string]any{}
	}

	params[paramCutoff] = cutoff
	params[paramLimit] = limit

	for i, col := range slices.Sorted(maps.Keys(prep.rule.Set)) {
		params[fmt.Sprintf("%s%d", paramPrefix, i)] = prep.rule.Set[col]
	}

	return params
}

// Run applies the selected rules in order. Every cutoff comes from one reading
// of the clock, so a long run does not shift its own windows. A rule that fails
// does not stop the others: its Result carries the error and Run returns all of
// them joined. A cancelled ctx stops the run between batches and Run returns the
// report so far with ctx's error. An unknown name in opts.Only is an error
// wrapping ErrUnknownRule, before anything runs.
func (p *Pruner) Run(ctx context.Context, opts Options) (Report, error) {
	rules, err := p.pick(opts.Only)
	if err != nil {
		return Report{}, err
	}

	now := p.clock.Now()
	report := Report{DryRun: opts.DryRun, Results: make([]Result, 0, len(rules))}

	var errs []error

	for _, prep := range rules {
		err := ctx.Err()
		if err != nil {
			return report, fmt.Errorf("retention: %w", err)
		}

		result := p.runRule(ctx, prep, now, opts.DryRun)
		if result.Err != nil {
			errs = append(errs, fmt.Errorf("retention %s: %w", prep.rule.Name, result.Err))
		}

		report.Results = append(report.Results, result)
	}

	return report, errors.Join(errs...)
}

func (p *Pruner) runRule(ctx context.Context, prep prepared, now time.Time, dry bool) Result {
	result := Result{Rule: prep.rule, Cutoff: now.Add(-prep.rule.Keep), Matched: 0, Affected: 0, Err: nil}

	err := p.conn.WithContext(ctx).Raw(prep.count, prep.params(result.Cutoff, 0)).Scan(&result.Matched).Error
	if err != nil {
		result.Err = fmt.Errorf("count %s: %w", prep.rule.Table, err)

		return result
	}

	if dry {
		return result
	}

	// Batches until one comes back short, so a backlog of any size clears in one
	// run without a long statement.
	for {
		err := ctx.Err()
		if err != nil {
			result.Err = fmt.Errorf("stopped after %d rows: %w", result.Affected, err)

			return result
		}

		res := p.conn.WithContext(ctx).Exec(prep.apply, prep.params(result.Cutoff, batchSize))
		if res.Error != nil {
			result.Err = fmt.Errorf("prune %s: %w", prep.rule.Table, res.Error)

			return result
		}

		result.Affected += res.RowsAffected

		if res.RowsAffected < batchSize {
			return result
		}
	}
}

// pick returns the rules to run, in the order they were given to New.
func (p *Pruner) pick(only []string) ([]prepared, error) {
	if len(only) == 0 {
		return p.rules, nil
	}

	for _, name := range only {
		if !slices.ContainsFunc(p.rules, func(prep prepared) bool { return prep.rule.Name == name }) {
			return nil, fmt.Errorf("retention: %w: %q", ErrUnknownRule, name)
		}
	}

	selected := slices.Clone(p.rules)

	return slices.DeleteFunc(selected, func(prep prepared) bool {
		return !slices.Contains(only, prep.rule.Name)
	}), nil
}
