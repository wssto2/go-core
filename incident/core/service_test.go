//nolint:exhaustruct,exhaustruct_v5,goconst,lll,paralleltest,thelper,varnamelen // tests in the style of the package's others
package core_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/incident/gormstore"
	"github.com/wssto2/go-core/incident/migrations"
	"gorm.io/gorm"
)

type clock struct{}

func (clock) Now() time.Time { return time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC) }

type nobody struct{}

func (nobody) Find(context.Context, []int) ([]core.Person, error) { return nil, nil }

// serviceOn creates the tables the way each database gets them and returns the service over them.
func serviceOn(t *testing.T, db *gorm.DB) (*core.Service, *gormstore.Store) {
	t.Helper()

	if db.Name() == "sqlite" {
		require.NoError(t, gormstore.Migrate(db))
	} else {
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("scratch", db)

		require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Up(context.Background()))
	}

	store := gormstore.New(db)

	return core.New(core.Config{Store: store, Transactor: database.NewTransactor(db), People: nobody{}, Clock: clock{}, Environment: "test"}), store
}

func count(t *testing.T, db *gorm.DB, table string) int {
	t.Helper()

	var n int64

	require.NoError(t, db.Table(table).Count(&n).Error)

	return int(n)
}

// A failure with more than the columns hold is cut to fit, so a capture is never lost to "data too long".
func TestACaptureIsCutToTheColumns(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		service, _ := serviceOn(t, db)

		err := service.Capture(t.Context(), core.Failure{
			Message: strings.Repeat("m", 70000), ErrorClass: strings.Repeat("c", 300), ErrorCode: strings.Repeat("e", 200),
			Stacktrace: strings.Repeat("s", 70000), AffectedModule: strings.Repeat("a", 200), AffectedRoute: strings.Repeat("r", 400),
			RequestMethod: strings.Repeat("g", 20), IPAddress: strings.Repeat("1", 80), RequestID: strings.Repeat("q", 200),
			Environment: strings.Repeat("v", 100), ContextJSON: "not json", RequestJSON: `{"a":1}`,
		})
		require.NoError(t, err)

		assert.Equal(t, 1, count(t, db, "incidents"))
		assert.Equal(t, 1, count(t, db, "incident_occurrences"))
	})
}

// The tables are utf8mb3: a character outside the Basic Multilingual Plane would be refused by MySQL, so it is replaced.
func TestACaptureOfAnEmojiIsStored(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		service, store := serviceOn(t, db)

		require.NoError(t, service.Capture(t.Context(), core.Failure{Message: "failed 😀 for Ćiro", AffectedModule: "čšž"}))

		found, err := store.Find(t.Context(), 1, core.Viewer{Staff: true})
		require.NoError(t, err)
		assert.Equal(t, "failed � for Ćiro", found.ErrorMessageSample)
		assert.Equal(t, "čšž", found.AffectedModule)

		_, err = service.Report(t.Context(), core.Viewer{UserID: 3}, core.ReportInput{Title: "Žaba 🐸", Description: "opis 🙂", FrontendErrorMessage: "x"})
		require.NoError(t, err)
	})
}

// Many requests failing at once with one error are one incident, however the database orders their writes.
func TestConcurrentCapturesOfOneErrorAreOneIncident(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		service, _ := serviceOn(t, db)

		const workers = 8

		var wg sync.WaitGroup

		errs := make(chan error, workers)

		for range workers {
			wg.Go(func() {
				errs <- service.Capture(t.Context(), core.Failure{Message: "everyone fails", AffectedModule: "m", AffectedRoute: "/r"})
			})
		}

		wg.Wait()
		close(errs)

		for err := range errs {
			require.NoError(t, err)
		}

		assert.Equal(t, 1, count(t, db, "incidents"))
		assert.Equal(t, workers, count(t, db, "incident_occurrences"))

		var counted int
		require.NoError(t, db.Table("incidents").Select("occurrence_count").Scan(&counted).Error)
		assert.Equal(t, workers, counted, "the first creates the incident with its first sighting, the others count theirs")
	})
}

// Capture announces a new incident inside its transaction, and a hook that fails rolls the incident back.
func TestANewIncidentIsAnnouncedInItsTransaction(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		_, store := serviceOn(t, db)

		var opened []core.Opened

		failing := true
		service := core.New(core.Config{
			Store: store, Transactor: database.NewTransactor(db), People: nobody{}, Clock: clock{},
			Opened: func(ctx context.Context, o core.Opened) error {
				_, inTx := database.TxFromContext(ctx)
				assert.True(t, inTx, "the hook runs in the transaction that creates the incident")

				opened = append(opened, o)

				if failing {
					return assert.AnError
				}

				return nil
			},
		})

		require.Error(t, service.Capture(t.Context(), core.Failure{Message: "boom"}))
		assert.Zero(t, count(t, db, "incidents"), "the incident was rolled back with the failed announcement")
		assert.Zero(t, count(t, db, "incident_occurrences"))

		failing = false

		require.NoError(t, service.Capture(t.Context(), core.Failure{Message: "boom"}))
		require.NoError(t, service.Capture(t.Context(), core.Failure{Message: "boom"}))
		assert.Len(t, opened, 2, "one failed attempt, one announcement; the repeat is not new")
		assert.Equal(t, core.SeverityHigh, opened[1].Severity)
		assert.Equal(t, core.SourceRuntimeError, opened[1].Source)
	})
}

// Comments and reports that arrive together on one incident are all kept and counted: the writes are ordered so that
// the database does not choose a victim among them, and a deadlock it chooses anyway is retried.
func TestConcurrentCommentsAndReportsAreAllKept(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		service, _ := serviceOn(t, db)

		first, err := service.Report(t.Context(), core.Viewer{UserID: 1}, core.ReportInput{Title: "Same", Description: "problem"})
		require.NoError(t, err)

		const workers = 8

		var wg sync.WaitGroup

		errs := make(chan error, 2*workers)

		for worker := range workers {
			who := core.Viewer{UserID: 10 + worker, Staff: true}

			wg.Go(func() {
				_, err := service.Comment(t.Context(), first.Incident.ID, who, core.CommentInput{Body: "me too"})
				errs <- err
			})
			wg.Go(func() {
				_, err := service.Report(t.Context(), who, core.ReportInput{Title: "Same", Description: "problem"})
				errs <- err
			})
		}

		wg.Wait()
		close(errs)

		for err := range errs {
			require.NoError(t, err)
		}

		shown, err := service.Show(t.Context(), first.Incident.ID, core.Viewer{UserID: 1, Staff: true})
		require.NoError(t, err)

		assert.Equal(t, 1, count(t, db, "incidents"))
		assert.Equal(t, workers, shown.Incident.CommentCount)
		assert.Equal(t, workers+1, shown.Incident.ReportCount)
		assert.Len(t, shown.Comments, workers)
		assert.Len(t, shown.Reports, workers+1)
	})
}
