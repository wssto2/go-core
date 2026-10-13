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

// errDown is the failure of the consumers that give up.
var errDown = errors.New("down")

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
				return errDown
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

// G21: told how long to keep dead letters, the housekeeper deletes a letter and the event only it kept once it is that
// old; a letter retried in time, or younger, stays. Without the option nothing changes: they are kept for ever.
func TestTheHousekeeperCanEndDeadLettersAfterTheTimeItWasToldToKeepThem(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, conn *gorm.DB) {
		t.Helper()

		publish := setup(t, conn)
		clock := newClock()
		ctx := context.Background()

		consumer := Assigned.To("notifications.assignee", func(_ context.Context, p TicketAssigned) error {
			if p.TicketID <= 3 {
				return errDown
			}

			return nil
		}).Retry(event.Attempts(1))
		queue := event.NewQueue(conn, clock, quiet(), consumer)

		forever := event.NewHousekeeper(conn, clock, quiet())
		ninety := event.NewHousekeeper(conn, clock, quiet()).KeepDeadLetters(90 * 24 * time.Hour)

		publish(TicketAssigned{TicketID: 1}) //nolint:exhaustruct,exhaustruct_v5 // zero by design
		publish(TicketAssigned{TicketID: 2}) //nolint:exhaustruct,exhaustruct_v5 // zero by design
		require.Error(t, queue.Drain(ctx))   // events 1 and 2 are dead letters

		clock.Advance(50 * 24 * time.Hour)
		publish(TicketAssigned{TicketID: 3}) //nolint:exhaustruct,exhaustruct_v5 // zero by design
		publish(TicketAssigned{TicketID: 4}) //nolint:exhaustruct,exhaustruct_v5 // zero by design
		require.Error(t, queue.Drain(ctx))   // event 3 is a dead letter 50 days younger; event 4 was handled

		// Event 1's letter is retried in time: it is not dead any more, so its age no longer counts.
		require.NoError(t, event.NewDeadLetters(conn, clock).Retry(ctx, 1, "notifications.assignee"))

		clock.Advance(41 * 24 * time.Hour) // event 2's letter is 91 days old, event 3's 41

		swept, err := forever.Sweep(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, swept, "only event 4, processed 41 days ago; the letters are kept for ever without the option")
		require.EqualValues(t, 3, count(t, conn, "outbox_events"))

		swept, err = ninety.Sweep(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, swept, "event 2, whose only reason to stay was a letter 91 days old")

		var left []uint64
		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		require.NoError(t, conn.Model(&event.OutboxEvent{}).Order("id").Pluck("id", &left).Error)
		require.Equal(t, []uint64{1, 3}, left, "the retried one and the letter younger than 90 days")
		require.EqualValues(t, 1, count(t, conn, "event_consumer_attempts WHERE dead_at IS NOT NULL"))

		clock.Advance(50 * 24 * time.Hour) // event 3's letter is 91 days old now

		swept, err = ninety.Sweep(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, swept)
		require.EqualValues(t, 1, count(t, conn, "outbox_events"), "event 1 waits to be handled")

		swept, err = event.NewHousekeeper(conn, clock, quiet()).KeepDeadLetters(0).Sweep(ctx)
		require.NoError(t, err)
		require.Zero(t, swept, "zero keeps letters for ever")
	})
}
