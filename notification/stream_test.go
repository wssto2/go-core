package notification_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/notification"
	"gorm.io/gorm"
)

// next returns the next event of the stream, failing the test when none comes.
func next(t *testing.T, events <-chan notification.StreamEvent) notification.StreamEvent {
	t.Helper()

	select {
	case ev, open := <-events:
		require.True(t, open, "the stream was closed")

		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event on the stream")

		return notification.StreamEvent{}
	}
}

func quiet(t *testing.T, events <-chan notification.StreamEvent) {
	t.Helper()

	select {
	case ev := <-events:
		t.Fatalf("the stream got %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}

// NOTIF-READ-001 rule 3: the open apps hear of a new notification with the fresh unread count, and of a
// reading on another device; other people's apps hear nothing.
func TestTheStreamHearsSendAndReadAfterCommit(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := seeded(t, db)

		mine, cancelMine := w.notices.Inbox.Subscribe(t.Context(), 1)
		defer cancelMine()

		other, cancelOther := w.notices.Inbox.Subscribe(t.Context(), 3)
		defer cancelOther()

		w.publish(assignedEvent{TicketID: 9, To: []int{1}})
		require.NoError(t, w.drain())

		created := next(t, mine)
		require.Equal(t, notification.StreamCreated, created.Kind)
		require.Equal(t, 7, created.Notification.ID)
		require.Equal(t, "/tickets/9", created.Notification.Link)
		require.Equal(t, 6, created.UnreadCount)
		quiet(t, other)

		_, err := w.notices.Inbox.MarkRead(t.Context(), 1, 7)
		require.NoError(t, err)

		read := next(t, mine)
		require.Equal(t, notification.StreamRead, read.Kind)
		require.Equal(t, []int{7}, read.ReadIDs)
		require.Equal(t, 5, read.UnreadCount)

		_, err = w.notices.Inbox.MarkRead(t.Context(), 1, 7) // already read: nothing changed, nothing announced
		require.NoError(t, err)

		_, err = w.notices.Inbox.MarkAllRead(t.Context(), 1, 4)
		require.NoError(t, err)

		all := next(t, mine)
		require.True(t, all.AllRead)
		require.Equal(t, 4, all.ReadUpToID)
		require.Equal(t, 1, all.UnreadCount)
		quiet(t, mine)
	})
}

// A Send that rolls back tells nobody; the retry that commits does, once.
func TestTheStreamHearsNothingOnRollbackAndOnceOnTheRetry(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)
		w.consumeAssigned()
		w.start()

		var inserts atomic.Int32

		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail-second-notification", func(tx *gorm.DB) {
			if tx.Statement.Table == "notifications" && inserts.Add(1) == 2 {
				_ = tx.AddError(errors.New("the disk is full"))
			}
		}))

		first, cancelFirst := w.notices.Inbox.Subscribe(t.Context(), 1)
		defer cancelFirst()

		w.publish(assignedEvent{To: []int{1, 2}})
		require.ErrorContains(t, w.drain(), "the disk is full")

		require.Zero(t, w.count("notifications"), "Ana's notification was rolled back with Ivo's")
		quiet(t, first)

		w.clock.Advance(time.Minute)
		require.NoError(t, w.drain())

		require.Equal(t, int64(2), w.count("notifications"))
		require.Equal(t, 1, next(t, first).UnreadCount)
		quiet(t, first)
	})
}

func TestAHubDropsASubscriberThatLagsAndKeepsTheRest(t *testing.T) {
	hub := notification.NewHub()
	ctx := context.Background()

	slow, _ := hub.Subscribe(ctx, 1)
	fast, cancelFast := hub.Subscribe(ctx, 1)

	defer cancelFast()

	for i := range 40 { // the buffer holds 32
		hub.Publish(ctx, 1, notification.StreamEvent{Kind: notification.StreamUnread, UnreadCount: i})

		if i < 32 {
			require.Equal(t, i, (<-fast).UnreadCount)
		}
	}

	require.Equal(t, 1, hub.Subscribers(1), "the lagging one was dropped")

	got := 0
	for range slow { // closed by the hub: ends after what was buffered
		got++
	}

	require.Equal(t, 32, got)
}

func TestCancelingASubscriptionTwiceIsHarmless(t *testing.T) {
	hub := notification.NewHub()
	events, cancel := hub.Subscribe(context.Background(), 1)

	cancel()
	cancel()

	_, open := <-events
	require.False(t, open)
	require.Zero(t, hub.Subscribers(1))
}
