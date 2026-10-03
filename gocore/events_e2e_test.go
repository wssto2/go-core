package gocore

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"gorm.io/gorm"
)

// A feature that publishes: its write and its event commit together.
type ticketsFeature struct {
	transactor database.Transactor
	db         *gorm.DB
}

func installTickets(app *App) *ticketsFeature {
	app.Events() // publisher only: the queue's tables, no consumer

	return &ticketsFeature{transactor: database.NewTransactor(app.Database()), db: app.Database()}
}

func (f *ticketsFeature) Assign(ctx context.Context, ticket, user int) error {
	return f.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		tx, _ := database.TxFromContext(ctx)
		if err := tx.Exec("INSERT INTO assignments (ticket_id, user_id) VALUES (?, ?)", ticket, user).Error; err != nil {
			return err
		}

		return assigned.Publish(ctx, ticketAssigned{TicketID: ticket})
	})
}

// A feature that consumes: it knows the event, not the feature that publishes it.
func installNotices(app *App, sent *[]int) {
	app.Events(assigned.To("notifications.assignee", func(_ context.Context, a ticketAssigned) error {
		*sent = append(*sent, a.TicketID)

		return nil
	}))
}

// One module publishes, another consumes, on SQLite, MySQL and MariaDB, the
// servers with the real migration files.
func TestEventsBetweenTwoFeaturesOnEveryDatabase(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("local", db)

		app := New(bootstrap.DefaultConfig(), WithRegistry(reg), WithLogger(slog.New(slog.DiscardHandler)), WithAutoMigrate(t.Context()))
		require.NoError(t, db.Exec("CREATE TABLE assignments (ticket_id INT, user_id INT)").Error)

		var sent []int

		tickets := installTickets(app)
		installNotices(app, &sent)

		require.NoError(t, app.Migrate(t.Context()))
		require.NoError(t, app.Check())

		require.NoError(t, tickets.Assign(t.Context(), 7, 3))
		require.NoError(t, app.DrainEvents(t.Context()))
		require.Equal(t, []int{7}, sent)

		require.NoError(t, app.DrainEvents(t.Context()))
		require.Equal(t, []int{7}, sent, "handled once")
	})
}
