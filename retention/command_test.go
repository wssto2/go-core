//nolint:err113,exhaustruct,exhaustruct_v5,goconst,lll,testpackage // tests in the style of the package's others
package retention

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
)

const testDay = 24 * time.Hour

func TestPrintReport(t *testing.T) {
	t.Parallel()

	cutoff := time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC)
	sessions := Rule{Name: "sessions", Keep: 7 * testDay}
	raw := Rule{Name: "leadimportrawdata", Keep: 90 * testDay, Set: map[string]any{"raw_data": "[]"}}

	t.Run("dry run prints the matched count", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		printReport(&out, Report{DryRun: true, Results: []Result{
			{Rule: sessions, Cutoff: cutoff, Matched: 12},
			{Rule: raw, Cutoff: cutoff, Matched: 3},
		}})

		assert.Contains(t, out.String(), "Dry run — nothing is changed.")
		assert.Contains(t, out.String(), "sessions                 keep   7d  before 2026-10-09  12 to delete")
		assert.Contains(t, out.String(), "leadimportrawdata        keep  90d  before 2026-10-09  3 to redact")
	})

	t.Run("a real run prints the affected count and a failure its error", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		printReport(&out, Report{Results: []Result{
			{Rule: sessions, Cutoff: cutoff, Matched: 12, Affected: 12},
			{Rule: raw, Cutoff: cutoff, Matched: 3, Affected: 1, Err: errors.New("boom")},
		}})

		assert.NotContains(t, out.String(), "Dry run")
		assert.Contains(t, out.String(), "  12 deleted")
		assert.Contains(t, out.String(), "before 2026-10-09  boom")
	})
}

func commandApp(t *testing.T) *gocore.App {
	t.Helper()

	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	app := gocore.New(gocore.WithConfig(gocore.DefaultConfig()), gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)))
	Command(app, "local", func() []Rule {
		return []Rule{{Name: "sessions", Table: "sessions", Keep: 7 * testDay, Where: "created_at < @cutoff"}}
	}, "delete rows past their window", "Delete old rows.")

	return app
}

func TestCommandRefusesAnUnknownOnlyName(t *testing.T) {
	t.Parallel()

	err := commandApp(t).RunCommand(context.Background(), []string{"db:prune", "-only", "sessions,nope"}, &bytes.Buffer{})
	require.ErrorIs(t, err, ErrUnknownRule)
}

func TestCommandHelpHasItsTextAndFlags(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	require.NoError(t, commandApp(t).RunCommand(context.Background(), []string{"db:prune", "-h"}, &out))
	assert.Contains(t, out.String(), "Delete old rows.")
	assert.Contains(t, out.String(), "-dry-run")
	assert.Contains(t, out.String(), "-only")
}
