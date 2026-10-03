package event_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/event"
	"gorm.io/gorm"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

func newClock() *fakeClock { return &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)} }

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

// setup creates the tables and returns a publish function.
func setup(t *testing.T, db *gorm.DB) func(TicketAssigned) {
	t.Helper()
	require.NoError(t, event.Migrate(db))

	transactor := database.NewTransactor(db)

	return func(p TicketAssigned) {
		t.Helper()
		require.NoError(t, transactor.WithinTransaction(context.Background(), func(ctx context.Context) error {
			return Assigned.Publish(ctx, p)
		}))
	}
}

func processed(t *testing.T, db *gorm.DB, id uint64) bool {
	t.Helper()

	var n int64

	require.NoError(t, db.Model(&event.OutboxEvent{}).Where("id = ? AND processed_at IS NOT NULL", id).Count(&n).Error)

	return n == 1
}

type attemptState struct {
	Attempts  int
	LastError string
	DoneAt    *time.Time
	DeadAt    *time.Time
}

func stateOf(t *testing.T, db *gorm.DB, id uint64, consumer string) attemptState {
	t.Helper()

	var s attemptState

	require.NoError(t, db.Table("event_consumer_attempts").Where("event_id = ? AND consumer = ?", id, consumer).Take(&s).Error)

	return s
}

func TestAHandledEventIsDoneOnce(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		publish := setup(t, db)

		var got []TicketAssigned

		c := Assigned.To("notifications.assignee", func(ctx context.Context, p TicketAssigned) error {
			id, ok := event.ID(ctx)
			require.True(t, ok)
			require.EqualValues(t, 1, id)

			got = append(got, p)

			return nil
		})
		q := event.NewQueue(db, newClock(), quiet(), c)

		publish(TicketAssigned{TicketID: 7, UserID: 3})
		require.NoError(t, q.Drain(context.Background()))
		require.NoError(t, q.Drain(context.Background()))

		require.Equal(t, []TicketAssigned{{TicketID: 7, UserID: 3}}, got)
		require.NotNil(t, stateOf(t, db, 1, "notifications.assignee").DoneAt)
		require.True(t, processed(t, db, 1))
	})
}

func TestAFailingEventIsRetriedWithBackoffThenDeadLettered(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		publish := setup(t, db)
		clock := newClock()
		calls := 0

		c := Assigned.To("notifications.assignee", func(context.Context, TicketAssigned) error {
			calls++

			return errors.New("smtp down")
		}).Retry(event.Attempts(3), event.Backoff(5*time.Second, time.Minute))
		q := event.NewQueue(db, clock, quiet(), c)

		publish(TicketAssigned{TicketID: 1})

		err := q.Drain(context.Background())
		require.ErrorContains(t, err, "smtp down")
		require.Equal(t, 1, calls)
		require.Equal(t, 1, stateOf(t, db, 1, "notifications.assignee").Attempts)

		// Not due yet.
		clock.Advance(4 * time.Second)
		require.NoError(t, q.Drain(context.Background()))
		require.Equal(t, 1, calls)

		clock.Advance(time.Second) // 5 s after the first failure
		require.Error(t, q.Drain(context.Background()))
		require.Equal(t, 2, calls)

		clock.Advance(9 * time.Second) // the second delay is 10 s
		require.NoError(t, q.Drain(context.Background()))
		require.Equal(t, 2, calls)

		clock.Advance(time.Second)
		require.Error(t, q.Drain(context.Background()))
		require.Equal(t, 3, calls)

		s := stateOf(t, db, 1, "notifications.assignee")
		require.NotNil(t, s.DeadAt, "the third failure sets it aside")
		require.Equal(t, "smtp down", s.LastError)
		require.True(t, processed(t, db, 1))

		clock.Advance(time.Hour)
		require.NoError(t, q.Drain(context.Background()))
		require.Equal(t, 3, calls, "a dead letter is not handled again")
	})
}

func TestAnUnreadablePayloadIsDeadLetteredAtOnce(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		publish := setup(t, db)
		calls := 0
		c := Assigned.To("notifications.assignee", func(context.Context, TicketAssigned) error { calls++; return nil })
		q := event.NewQueue(db, newClock(), quiet(), c)

		publish(TicketAssigned{TicketID: 1})
		require.NoError(t, db.Model(&event.OutboxEvent{}).Where("id = 1").
			Update("envelope", `{"_v":"1","payload":{"ticket_id":"not a number"}}`).Error)

		require.Error(t, q.Drain(context.Background()))
		require.Zero(t, calls)

		s := stateOf(t, db, 1, "notifications.assignee")
		require.NotNil(t, s.DeadAt)
		require.Equal(t, 1, s.Attempts)
		require.Contains(t, s.LastError, "malformed event")
	})
}

func TestAnEventOfAnotherVersionIsDeadLettered(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		publish := setup(t, db)
		c := Assigned.Version(2).To("notifications.assignee", func(context.Context, TicketAssigned) error { return nil })
		q := event.NewQueue(db, newClock(), quiet(), c)

		publish(TicketAssigned{TicketID: 1}) // written as version 1

		require.ErrorContains(t, q.Drain(context.Background()), "written as version")
		require.NotNil(t, stateOf(t, db, 1, "notifications.assignee").DeadAt)
	})
}

func TestAPanickingHandlerIsAFailureNotACrash(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		publish := setup(t, db)
		c := Assigned.To("notifications.assignee", func(context.Context, TicketAssigned) error { panic("boom") })
		q := event.NewQueue(db, newClock(), quiet(), c)

		publish(TicketAssigned{TicketID: 1})
		require.ErrorContains(t, q.Drain(context.Background()), "boom")
		require.Equal(t, 1, stateOf(t, db, 1, "notifications.assignee").Attempts)
	}, dbtest.SQLite)
}

// One failing consumer holds back no other, and the outbox row is processed
// only when every consumer finished.
func TestConsumersAreIndependent(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		publish := setup(t, db)
		clock := newClock()
		healthy, flaky := 0, 0

		good := Assigned.To("audit.trail", func(context.Context, TicketAssigned) error { healthy++; return nil })
		bad := Assigned.To("notifications.assignee", func(context.Context, TicketAssigned) error {
			flaky++
			if flaky < 2 {
				return errors.New("not yet")
			}

			return nil
		})
		q := event.NewQueue(db, clock, quiet(), good, bad)

		publish(TicketAssigned{TicketID: 1})
		require.Error(t, q.Drain(context.Background()))
		require.Equal(t, 1, healthy)
		require.False(t, processed(t, db, 1), "one consumer is still waiting")

		clock.Advance(5 * time.Second)
		require.NoError(t, q.Drain(context.Background()))
		require.Equal(t, 1, healthy, "the finished consumer is not called again")
		require.Equal(t, 2, flaky)
		require.True(t, processed(t, db, 1))
	})
}

func TestDeadLettersAreListedAndRetried(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		publish := setup(t, db)
		clock := newClock()
		fail := true

		c := Assigned.To("notifications.assignee", func(context.Context, TicketAssigned) error {
			if fail {
				return errors.New("down")
			}

			return nil
		}).Retry(event.Attempts(1))
		q := event.NewQueue(db, clock, quiet(), c)
		dead := event.NewDeadLetters(db, clock)

		publish(TicketAssigned{TicketID: 1})
		publish(TicketAssigned{TicketID: 2})
		require.Error(t, q.Drain(context.Background()))

		letters, total, err := dead.List(context.Background(), event.DeadLetterQuery{})
		require.NoError(t, err)
		require.Equal(t, 2, total)
		require.Len(t, letters, 2)
		require.EqualValues(t, 2, letters[0].EventID, "newest first")
		require.Equal(t, "tickets.assigned", letters[0].EventName)
		require.Equal(t, "down", letters[0].LastError)
		require.True(t, letters[0].Retryable())

		_, total, err = dead.List(context.Background(), event.DeadLetterQuery{Consumer: "someone.else"})
		require.NoError(t, err)
		require.Zero(t, total)

		fail = false

		require.NoError(t, dead.Retry(context.Background(), 1, "notifications.assignee"))
		require.ErrorIs(t, dead.Retry(context.Background(), 1, "notifications.assignee"), event.ErrDeadLetterNotFound)
		require.NoError(t, q.Drain(context.Background()))
		require.NotNil(t, stateOf(t, db, 1, "notifications.assignee").DoneAt)
		require.Nil(t, stateOf(t, db, 1, "notifications.assignee").DeadAt)
		require.True(t, processed(t, db, 1))

		n, err := dead.RetryAll(context.Background(), "notifications.assignee")
		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.NoError(t, q.Drain(context.Background()))

		_, total, err = dead.List(context.Background(), event.DeadLetterQuery{})
		require.NoError(t, err)
		require.Zero(t, total)

		require.NoError(t, db.Delete(&event.OutboxEvent{}, 1).Error)
		require.NoError(t, db.Table("event_consumer_attempts").Where("event_id = 1").Updates(map[string]any{"dead_at": clock.Now()}).Error)
		require.ErrorIs(t, dead.Retry(context.Background(), 1, "notifications.assignee"), event.ErrDeadLetterEventGone)

		letters, _, err = dead.List(context.Background(), event.DeadLetterQuery{})
		require.NoError(t, err)
		require.Len(t, letters, 1)
		require.False(t, letters[0].Retryable(), "a pruned event is listed but cannot be retried")
	})
}

// Two workers on one queue's database never handle one event twice in the
// normal case: the claim locks the outbox rows by primary key and leases them.
func TestTwoWorkersNeverHandleOneEventTwice(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		publish := setup(t, db)

		const events = 60

		for i := range events {
			publish(TicketAssigned{TicketID: i})
		}

		var (
			mu   sync.Mutex
			seen = map[int]int{}
			wg   sync.WaitGroup
		)

		handler := func(_ context.Context, p TicketAssigned) error {
			mu.Lock()
			seen[p.TicketID]++
			mu.Unlock()

			return nil
		}

		var failures atomic.Int32

		for range 3 {
			wg.Add(1)

			go func() {
				defer wg.Done()

				q := event.NewQueue(db, newClock(), quiet(), Assigned.To("notifications.assignee", handler))
				if err := q.Drain(context.Background()); err != nil {
					failures.Add(1)
					t.Error(err)
				}
			}()
		}

		wg.Wait()
		require.Zero(t, failures.Load())
		require.Len(t, seen, events)

		for id, n := range seen {
			require.Equal(t, 1, n, "ticket %d handled %d times", id, n)
		}
	})
}

func TestTheClaimSkipsLeasedEventsUntilTheLeaseEnds(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		publish := setup(t, db)
		clock := newClock()
		publish(TicketAssigned{TicketID: 1})

		got := map[string]int{}
		handler := func(name string) event.Consumer {
			return Assigned.To("notifications.assignee", func(context.Context, TicketAssigned) error { got[name]++; return nil })
		}

		leased := event.NewQueue(db, clock, quiet(), handler("late"))

		// A worker that claimed the event and died: the lease row exists, nothing finished.
		now := clock.Now().Truncate(time.Second)
		require.NoError(t, db.Exec("INSERT INTO event_consumer_attempts (event_id, consumer, attempts, next_attempt_at, updated_at) VALUES (1, 'notifications.assignee', 0, ?, ?)",
			now.Add(5*time.Minute), now).Error)

		require.NoError(t, leased.Drain(context.Background()))
		require.Empty(t, got, "leased: left alone")

		clock.Advance(5*time.Minute + time.Second)
		require.NoError(t, leased.Drain(context.Background()))
		require.Equal(t, 1, got["late"], "the lease ended: the event is handled")
	})
}

func TestEventsOfOtherNamesAndAlreadyProcessedRowsAreLeftAlone(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		publish := setup(t, db)
		publish(TicketAssigned{TicketID: 1})
		require.NoError(t, db.Model(&event.OutboxEvent{}).Where("id = 1").Update("event_type", "tickets.closed").Error)
		publish(TicketAssigned{TicketID: 2})
		require.NoError(t, db.Model(&event.OutboxEvent{}).Where("id = 2").Update("processed_at", time.Now()).Error)

		calls := 0
		q := event.NewQueue(db, newClock(), quiet(), Assigned.To("notifications.assignee", func(context.Context, TicketAssigned) error { calls++; return nil }))
		require.NoError(t, q.Drain(context.Background()))
		require.Zero(t, calls)
	})
}
