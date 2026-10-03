package event_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/event"
	"gorm.io/gorm"
)

func count(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()

	var n int64
	require.NoError(t, db.Table(table).Count(&n).Error)

	return n
}

// Processed events and their attempts are deleted after 30 days; a dead letter's event, an event still
// waiting and a recent one stay.
func TestTheHousekeeperDeletesWhatWasProcessedMoreThanThirtyDaysAgo(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		publish := setup(t, db)
		clock := newClock()
		ctx := context.Background()

		c := Assigned.To("notifications.assignee", func(_ context.Context, p TicketAssigned) error {
			if p.TicketID == 3 {
				return errors.New("down")
			}

			return nil
		}).Retry(event.Attempts(1))
		q := event.NewQueue(db, clock, quiet(), c)
		house := event.NewHousekeeper(db, clock, quiet())

		for id := 1; id <= 4; id++ {
			publish(TicketAssigned{TicketID: id})
		}

		require.Error(t, q.Drain(ctx)) // ticket 3 is a dead letter

		clock.Advance(20 * 24 * time.Hour)
		publish(TicketAssigned{TicketID: 5})
		require.NoError(t, q.Drain(ctx))
		publish(TicketAssigned{TicketID: 6}) // waiting: never drained

		clock.Advance(10*24*time.Hour + time.Hour) // 30 days and an hour after the first four, ten days after the fifth

		n, err := house.Sweep(ctx)
		require.NoError(t, err)
		require.Equal(t, 3, n, "events 1, 2 and 4")

		var left []uint64
		require.NoError(t, db.Model(&event.OutboxEvent{}).Order("id").Pluck("id", &left).Error)
		require.Equal(t, []uint64{3, 5, 6}, left, "the dead letter's event, the recent one and the one still waiting")

		require.EqualValues(t, 2, count(t, db, "event_consumer_attempts"), "attempts of deleted events go with them")

		// The dead letter can still be retried: its event was kept.
		require.NoError(t, event.NewDeadLetters(db, clock).Retry(ctx, 3, "notifications.assignee"))

		n, err = house.Sweep(ctx)
		require.NoError(t, err)
		require.Zero(t, n)
	})
}

// A sweep handles more events than one batch holds.
func TestTheHousekeeperWorksInBatches(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, event.Migrate(db))

		clock := newClock()
		old := clock.Now().Add(-40 * 24 * time.Hour)

		rows := make([]event.OutboxEvent, 1100)
		for i := range rows {
			rows[i] = event.OutboxEvent{EventType: "x", Envelope: []byte(`{}`), CreatedAt: old, ProcessedAt: &old}
		}

		require.NoError(t, db.CreateInBatches(&rows, 200).Error)

		n, err := event.NewHousekeeper(db, clock, quiet()).Sweep(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1100, n)
		require.Zero(t, count(t, db, "outbox_events"), fmt.Sprint(n))
	})
}
