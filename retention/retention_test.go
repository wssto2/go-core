//nolint:exhaustruct,exhaustruct_v5,gochecknoglobals,lll,paralleltest,thelper,varnamelen // tests in the style of the package's others
package retention_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/retention"
	"gorm.io/gorm"
)

const (
	day     = 24 * time.Hour
	items   = "items"
	payload = "payload"
	second  = "second"
	table   = "table"
)

var now = time.Date(2026, time.March, 10, 3, 0, 0, 0, time.UTC)

type fixedClock struct{ calls atomic.Int32 }

func (c *fixedClock) Now() time.Time {
	c.calls.Add(1)

	return now
}

// servers is where the statements are checked: the two servers production runs on.
func servers(t *testing.T, fn func(t *testing.T, db *gorm.DB)) {
	t.Helper()

	dbtest.Run(t, fn, dbtest.MySQL, dbtest.MariaDB)
}

// world creates the tables: items (what the rules prune) and batches (a table a rule's subquery reads).
func world(t *testing.T, db *gorm.DB) {
	t.Helper()

	require.NoError(t, db.Exec("CREATE TABLE batches (id BIGINT AUTO_INCREMENT PRIMARY KEY, status INT NOT NULL)").Error)
	require.NoError(t, db.Exec("CREATE TABLE items ("+
		"id BIGINT AUTO_INCREMENT PRIMARY KEY, batch_id BIGINT NOT NULL, `key` VARCHAR(40) NOT NULL, "+
		"payload VARCHAR(40) NULL, created_at DATETIME NOT NULL)").Error)
}

// item inserts n rows created age ago.
func item(t *testing.T, db *gorm.DB, n, batchID int, payload string, age time.Duration) {
	t.Helper()

	created := now.Add(-age)
	rows := make([]string, 0, n)
	args := make([]any, 0, 4*n)

	for i := range n {
		rows = append(rows, "(?, ?, ?, ?)")
		args = append(args, batchID, fmt.Sprintf("k%d", i), payload, created)
	}

	require.NoError(t, db.Exec("INSERT INTO items (batch_id, `key`, payload, created_at) VALUES "+strings.Join(rows, ","), args...).Error)
}

func count(t *testing.T, db *gorm.DB, where string, args ...any) int64 {
	t.Helper()

	var n int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM items WHERE "+where, args...).Scan(&n).Error)

	return n
}

func newPruner(t *testing.T, db *gorm.DB, rules ...retention.Rule) *retention.Pruner {
	t.Helper()

	p, err := retention.New(db, &fixedClock{}, rules...)
	require.NoError(t, err)

	return p
}

var oldItems = retention.Rule{Name: "olditems", Table: items, Keep: 7 * day, Where: "created_at < @cutoff"}

func TestDeleteRemovesOnlyTheRowsPastTheWindow(t *testing.T) {
	servers(t, func(t *testing.T, db *gorm.DB) {
		world(t, db)
		item(t, db, 3, 1, "x", 10*day)
		item(t, db, 2, 1, "x", 7*day+time.Second)
		item(t, db, 4, 1, "x", 7*day) // exactly Keep old: the window is "older than"
		item(t, db, 5, 1, "x", 6*day)

		clock := &fixedClock{}
		p, err := retention.New(db, clock, oldItems)
		require.NoError(t, err)

		report, err := p.Run(t.Context(), retention.Options{})
		require.NoError(t, err)

		require.Len(t, report.Results, 1)
		assert.Equal(t, int64(5), report.Results[0].Matched)
		assert.Equal(t, int64(5), report.Results[0].Affected)
		assert.Equal(t, now.Add(-7*day), report.Results[0].Cutoff)
		assert.Equal(t, int32(1), clock.calls.Load(), "one reading of the clock for the whole run")
		assert.Equal(t, int64(9), count(t, db, "1 = 1"))
		assert.Zero(t, count(t, db, "created_at < ?", now.Add(-7*day)))

		again, err := p.Run(t.Context(), retention.Options{})
		require.NoError(t, err)
		assert.Zero(t, again.Results[0].Affected, "nothing is left to prune")
	})
}

func TestRedactKeepsTheRowsAndClearsTheColumn(t *testing.T) {
	servers(t, func(t *testing.T, db *gorm.DB) {
		world(t, db)
		require.NoError(t, db.Exec("INSERT INTO batches (id, status) VALUES (1, 3), (2, 4), (3, 1)").Error)
		item(t, db, 2, 1, "secret", 10*day) // finished batch, old: redacted
		item(t, db, 2, 2, "secret", 10*day) // finished batch, old: redacted
		item(t, db, 3, 3, "secret", 10*day) // batch still running: kept
		item(t, db, 4, 1, "secret", 2*day)  // recent: kept
		item(t, db, 1, 1, "[]", 10*day)     // already redacted: not matched

		p := newPruner(t, db, retention.Rule{
			Name: "payloads", Table: items, Keep: 7 * day,
			Set:   map[string]any{payload: "[]"},
			Where: "payload <> '[]' AND created_at < @cutoff AND batch_id IN (SELECT id FROM batches WHERE status IN @finished)",
			Args:  map[string]any{"finished": []int{3, 4}},
		})

		report, err := p.Run(t.Context(), retention.Options{})
		require.NoError(t, err)
		assert.Equal(t, int64(4), report.Results[0].Matched)
		assert.Equal(t, int64(4), report.Results[0].Affected)

		assert.Equal(t, int64(12), count(t, db, "1 = 1"), "redacting deletes nothing")
		assert.Equal(t, int64(3), count(t, db, "payload = 'secret' AND created_at < ?", now.Add(-7*day)), "only the unfinished batch keeps its old payloads")
		assert.Equal(t, int64(4), count(t, db, "payload = 'secret' AND created_at >= ?", now.Add(-7*day)), "and the recent rows")
		assert.Equal(t, int64(5), count(t, db, "payload = '[]'"))
	})
}

func TestRedactSetsSeveralColumnsAndNull(t *testing.T) {
	servers(t, func(t *testing.T, db *gorm.DB) {
		world(t, db)
		item(t, db, 2, 1, "secret", 10*day)

		p := newPruner(t, db, retention.Rule{
			Name: "wipe", Table: items, Keep: 7 * day,
			Set:   map[string]any{payload: nil, "key": "gone"},
			Where: "payload IS NOT NULL AND created_at < @cutoff",
		})

		report, err := p.Run(t.Context(), retention.Options{})
		require.NoError(t, err)
		assert.Equal(t, int64(2), report.Results[0].Affected)
		assert.Equal(t, int64(2), count(t, db, "payload IS NULL AND `key` = 'gone'"))
	})
}

func TestBatchesRunUntilOneComesBackShort(t *testing.T) {
	for _, n := range []int{1200, 1000, 499} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			servers(t, func(t *testing.T, db *gorm.DB) {
				world(t, db)
				item(t, db, n, 1, "x", 10*day)
				item(t, db, 7, 1, "x", day)

				var deletes atomic.Int64

				require.NoError(t, db.Callback().Raw().Register("test:count", func(tx *gorm.DB) {
					if strings.HasPrefix(tx.Statement.SQL.String(), "DELETE") {
						deletes.Add(1)
					}
				}))

				report, err := newPruner(t, db, oldItems).Run(t.Context(), retention.Options{})
				require.NoError(t, err)

				assert.Equal(t, int64(n), report.Results[0].Matched)
				assert.Equal(t, int64(n), report.Results[0].Affected)
				assert.Equal(t, int64(7), count(t, db, "1 = 1"))
				// 500 rows a statement; a short batch (possibly empty, after an exact multiple) ends the loop.
				assert.Equal(t, int64(n/500+1), deletes.Load())
			})
		})
	}
}

func TestDryRunCountsAndChangesNothing(t *testing.T) {
	servers(t, func(t *testing.T, db *gorm.DB) {
		world(t, db)
		item(t, db, 6, 1, "x", 10*day)
		item(t, db, 2, 1, "x", day)

		report, err := newPruner(t, db, oldItems).Run(t.Context(), retention.Options{DryRun: true})
		require.NoError(t, err)

		assert.True(t, report.DryRun)
		assert.Equal(t, int64(6), report.Results[0].Matched)
		assert.Zero(t, report.Results[0].Affected)
		assert.Equal(t, int64(8), count(t, db, "1 = 1"))
	})
}

func TestOnlyLimitsTheRunToTheNamedRules(t *testing.T) {
	servers(t, func(t *testing.T, db *gorm.DB) {
		world(t, db)
		item(t, db, 2, 1, "x", 10*day)
		item(t, db, 3, 2, "x", 10*day)

		p := newPruner(t, db,
			retention.Rule{Name: "first", Table: items, Keep: 7 * day, Where: "created_at < @cutoff AND batch_id = 1"},
			retention.Rule{Name: second, Table: items, Keep: 7 * day, Where: "created_at < @cutoff AND batch_id = 2"},
		)

		report, err := p.Run(t.Context(), retention.Options{Only: []string{second}})
		require.NoError(t, err)
		require.Len(t, report.Results, 1)
		assert.Equal(t, second, report.Results[0].Rule.Name)
		assert.Equal(t, int64(2), count(t, db, "1 = 1"), "the second rule's three rows went, the first rule's two stayed")

		report, err = p.Run(t.Context(), retention.Options{})
		require.NoError(t, err)
		assert.Equal(t, []string{"first", second}, []string{report.Results[0].Rule.Name, report.Results[1].Rule.Name}, "the order they were given")

		_, err = p.Run(t.Context(), retention.Options{Only: []string{"nope"}})
		require.ErrorContains(t, err, `unknown rule: "nope"`)
	})
}

func TestARuleThatFailsDoesNotStopTheOthers(t *testing.T) {
	servers(t, func(t *testing.T, db *gorm.DB) {
		world(t, db)
		item(t, db, 3, 1, "x", 10*day)

		p := newPruner(t, db,
			retention.Rule{Name: "broken", Table: items, Keep: day, Where: "no_such_column < @cutoff"},
			oldItems,
		)

		report, err := p.Run(t.Context(), retention.Options{})
		require.ErrorContains(t, err, "retention broken")
		require.Len(t, report.Results, 2)
		require.Error(t, report.Results[0].Err)
		require.NoError(t, report.Results[1].Err)
		assert.Equal(t, int64(3), report.Results[1].Affected)
	})
}

func TestACancelledContextStopsTheRun(t *testing.T) {
	servers(t, func(t *testing.T, db *gorm.DB) {
		world(t, db)
		item(t, db, 3, 1, "x", 10*day)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		report, err := newPruner(t, db, oldItems).Run(ctx, retention.Options{})
		require.ErrorIs(t, err, ctx.Err())
		assert.Empty(t, report.Results)
		assert.Equal(t, int64(3), count(t, db, "1 = 1"))
	})
}

// changed is oldItems after change.
func changed(change func(r *retention.Rule)) retention.Rule {
	r := oldItems
	change(&r)

	return r
}

func TestNewRefusesARuleThatCannotRunAsWritten(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(r *retention.Rule)
		want   string
	}{
		"a table with SQL in it":     {func(r *retention.Rule) { r.Table = "items; DROP TABLE users" }, table},
		"a table with a schema":      {func(r *retention.Rule) { r.Table = "other.items" }, table},
		"an empty table":             {func(r *retention.Rule) { r.Table = "" }, table},
		"an order column with SQL":   {func(r *retention.Rule) { r.OrderBy = "id DESC, (SELECT 1)" }, "order column"},
		"a Set column with SQL":      {func(r *retention.Rule) { r.Set = map[string]any{"a = 1, b": 2} }, "Set column"},
		"an empty Set":               {func(r *retention.Rule) { r.Set = map[string]any{} }, "empty Set"},
		"no Where":                   {func(r *retention.Rule) { r.Where = "" }, "@cutoff"},
		"a Where without @cutoff":    {func(r *retention.Rule) { r.Where = "status = 1" }, "@cutoff"},
		"no Keep":                    {func(r *retention.Rule) { r.Keep = 0 }, "Keep"},
		"a negative Keep":            {func(r *retention.Rule) { r.Keep = -day }, "Keep"},
		"a reserved argument":        {func(r *retention.Rule) { r.Args = map[string]any{"cutoff": 1} }, "reserved"},
		"a reserved argument prefix": {func(r *retention.Rule) { r.Args = map[string]any{"retention_0": 1} }, "reserved"},
		"no name":                    {func(r *retention.Rule) { r.Name = "" }, "name"},
		"a name with a comma":        {func(r *retention.Rule) { r.Name = "a,b" }, "name"},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := retention.New(new(gorm.DB), nil, changed(tc.change))
			require.ErrorIs(t, err, retention.ErrInvalidRule)
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, p)
		})
	}
}

func TestNewChecksTheRulesAsAWhole(t *testing.T) {
	t.Run("a duplicate name", func(t *testing.T) {
		_, err := retention.New(new(gorm.DB), nil, oldItems, oldItems)
		require.ErrorIs(t, err, retention.ErrInvalidRule)
		require.ErrorContains(t, err, "defined twice")
	})

	t.Run("every problem at once", func(t *testing.T) {
		_, err := retention.New(new(gorm.DB), nil,
			changed(func(r *retention.Rule) { r.Table = "a b" }),
			changed(func(r *retention.Rule) { r.Name = "other"; r.Keep = 0 }),
		)
		require.ErrorContains(t, err, table)
		require.ErrorContains(t, err, "Keep")
	})

	t.Run("a valid set is accepted", func(t *testing.T) {
		p, err := retention.New(new(gorm.DB), nil, oldItems, changed(func(r *retention.Rule) {
			r.Name = "redact"
			r.OrderBy = "created_at"
			r.Set = map[string]any{payload: nil}
			r.Args = map[string]any{"done": []int{1}}
		}))
		require.NoError(t, err)
		assert.NotNil(t, p)
	})

	t.Run("no database", func(t *testing.T) {
		_, err := retention.New(nil, nil, oldItems)
		require.ErrorIs(t, err, retention.ErrNoDatabase)
	})
}
