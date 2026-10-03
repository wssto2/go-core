package event_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/event"
	"gorm.io/gorm"
)

type TicketAssigned struct {
	TicketID int `json:"ticket_id"`
	UserID   int `json:"user_id"`
}

var Assigned = event.Define[TicketAssigned]("tickets.assigned")

func TestPublishJoinsTheTransactionInContext(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, event.EnsureOutboxSchema(db))
		transactor := database.NewTransactor(db)

		require.NoError(t, transactor.WithinTransaction(context.Background(), func(ctx context.Context) error {
			return Assigned.Publish(ctx, TicketAssigned{TicketID: 7, UserID: 3})
		}))

		errRollback := errors.New("rollback")
		require.ErrorIs(t, transactor.WithinTransaction(context.Background(), func(ctx context.Context) error {
			require.NoError(t, Assigned.Publish(ctx, TicketAssigned{TicketID: 8}))

			return errRollback
		}), errRollback)

		var rows []event.OutboxEvent

		require.NoError(t, db.Find(&rows).Error)
		require.Len(t, rows, 1, "the rolled-back publish left nothing")
		require.Equal(t, "tickets.assigned", rows[0].EventType)

		var env event.Envelope

		require.NoError(t, json.Unmarshal(rows[0].Envelope, &env))
		require.Equal(t, "1", env.Version)
		require.JSONEq(t, `{"ticket_id":7,"user_id":3}`, string(env.Payload))
	})
}

func TestVersionIsWrittenWithTheEvent(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, event.EnsureOutboxSchema(db))

		v2 := Assigned.Version(2)
		require.NoError(t, database.NewTransactor(db).WithinTransaction(context.Background(), func(ctx context.Context) error {
			return v2.Publish(ctx, TicketAssigned{TicketID: 1})
		}))

		var row event.OutboxEvent

		require.NoError(t, db.Take(&row).Error)

		var env event.Envelope

		require.NoError(t, json.Unmarshal(row.Envelope, &env))
		require.Equal(t, "2", env.Version)
	}, dbtest.SQLite)
}

func TestPublishOutsideATransactionSaysWhatToDo(t *testing.T) {
	err := Assigned.Publish(context.Background(), TicketAssigned{})
	require.ErrorContains(t, err, "WithinTransaction")
	require.ErrorContains(t, err, "tickets.assigned")
}

func TestProblems(t *testing.T) {
	ok := func(context.Context, TicketAssigned) error { return nil }
	type Other struct{}

	cases := map[string][]event.Consumer{
		"two consumers are named":  {Assigned.To("a.b", ok), Assigned.To("a.b", ok)},
		"is not lower-case words":  {Assigned.To("Notices", ok)},
		"is defined twice":         {Assigned.To("a", ok), event.Define[Other]("tickets.assigned").To("b", func(context.Context, Other) error { return nil })},
		"retry policy that cannot": {Assigned.To("a", ok).Retry(event.Attempts(0))},
		"versions start at 1":      {Assigned.Version(0).To("a", ok)},
	}

	for want, consumers := range cases {
		require.Contains(t, fmt.Sprint(event.Problems(consumers)), want)
	}

	require.Empty(t, event.Problems([]event.Consumer{Assigned.To("notifications.assignee", ok), Assigned.To("audit-trail", ok)}))
}
