package notification_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/notification"
	"gorm.io/gorm"
)

// seeded is a world where Ana has five notifications (ids 1 to 5) and Ivo one (6).
func seeded(t *testing.T, db *gorm.DB) *world { return seededAs(t, db, nil) }

// seededAs is seeded in an application whose authorizer is the one given.
func seededAs(t *testing.T, db *gorm.DB, authorizer authz.Authorizer) *world {
	t.Helper()

	w := newWorld(t, db, authorizer)
	w.consumeAssigned()
	w.start()

	for range 5 {
		w.publish(assignedEvent{To: []int{1}})
	}

	w.publish(assignedEvent{To: []int{2}})
	require.NoError(t, w.drain())

	return w
}

func ids(page notification.Page) []int {
	out := []int{}
	for _, n := range page.Items {
		out = append(out, n.ID)
	}

	return out
}

// The inbox is newest first, a page at a time, with a before-id cursor and HasMore.
func TestTheInboxPagesNewestFirstByACursor(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := seeded(t, db)
		inbox := w.notices.Inbox

		first, err := inbox.List(t.Context(), 1, notification.ListQuery{Limit: 2})
		require.NoError(t, err)
		require.Equal(t, []int{5, 4}, ids(first))
		require.True(t, first.HasMore)

		second, err := inbox.List(t.Context(), 1, notification.ListQuery{Limit: 2, BeforeID: 4})
		require.NoError(t, err)
		require.Equal(t, []int{3, 2}, ids(second))
		require.True(t, second.HasMore)

		last, err := inbox.List(t.Context(), 1, notification.ListQuery{Limit: 2, BeforeID: 2})
		require.NoError(t, err)
		require.Equal(t, []int{1}, ids(last))
		require.False(t, last.HasMore)

		// Nothing else of Ana's: Ivo's notification is his.
		all, err := inbox.List(t.Context(), 1, notification.ListQuery{})
		require.NoError(t, err)
		require.Equal(t, []int{5, 4, 3, 2, 1}, ids(all))

		nobody, err := inbox.List(t.Context(), 3, notification.ListQuery{})
		require.NoError(t, err)
		require.NotNil(t, nobody.Items)
		require.Empty(t, nobody.Items)
		require.False(t, nobody.HasMore)
	})
}

func TestThePageSizeDefaultsAndIsClamped(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)
		w.consumeAssigned()
		w.start()

		for range notification.MaxPageSize + 5 {
			w.publish(assignedEvent{To: []int{1}})
		}

		require.NoError(t, w.drain())

		page, err := w.notices.Inbox.List(t.Context(), 1, notification.ListQuery{})
		require.NoError(t, err)
		require.Len(t, page.Items, notification.DefaultPageSize)

		page, err = w.notices.Inbox.List(t.Context(), 1, notification.ListQuery{Limit: 1000, BeforeID: -5})
		require.NoError(t, err)
		require.Len(t, page.Items, notification.MaxPageSize)
		require.True(t, page.HasMore)
	})
}

// NOTIF-READ-001 rule 1: read state is on the notification, so it is the same on every device; it is the person's own.
func TestReadingOneMarksItReadForThatPersonOnly(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := seeded(t, db)
		inbox := w.notices.Inbox

		unread, err := inbox.Unread(t.Context(), 1)
		require.NoError(t, err)
		require.Equal(t, 5, unread)

		left, err := inbox.MarkRead(t.Context(), 1, 3)
		require.NoError(t, err)
		require.Equal(t, 4, left)

		w.clock.Advance(time.Hour)

		left, err = inbox.MarkRead(t.Context(), 1, 3)
		require.NoError(t, err, "reading again changes nothing")
		require.Equal(t, 4, left)

		var readAt []time.Time
		require.NoError(t, w.db.Table("notifications").Where("id = 3").Pluck("read_at", &readAt).Error)
		require.Equal(t, w.clock.Now().Add(-time.Hour).UTC(), readAt[0].UTC(), "the first reading is kept")

		page, _ := inbox.List(t.Context(), 1, notification.ListQuery{})
		require.NotNil(t, page.Items[2].ReadAt, "the third newest is notification 3")
		require.Nil(t, page.Items[0].ReadAt)

		// Ivo's notification is not Ana's to read, and says nothing more than "not found".
		_, err = inbox.MarkRead(t.Context(), 1, 6)
		var app *apperr.AppError
		require.True(t, errors.As(err, &app))
		require.Equal(t, http.StatusNotFound, apperr.GetHTTPStatus(err))

		_, err = inbox.MarkRead(t.Context(), 1, 999)
		require.Equal(t, http.StatusNotFound, apperr.GetHTTPStatus(err))

		ivo, _ := inbox.Unread(t.Context(), 2)
		require.Equal(t, 1, ivo)
	})
}

// NOTIF-READ-001 rule 2: "mark all" marks what the person has seen; with nothing seen, nothing is marked.
func TestMarkAllReadMarksOnlyUpToWhatThePersonSaw(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := seeded(t, db)
		inbox := w.notices.Inbox

		left, err := inbox.MarkAllRead(t.Context(), 1, 0)
		require.NoError(t, err)
		require.Equal(t, 5, left, "nothing seen: nothing marked, never all")

		left, err = inbox.MarkAllRead(t.Context(), 1, -4)
		require.NoError(t, err)
		require.Equal(t, 5, left)

		left, err = inbox.MarkAllRead(t.Context(), 1, 3)
		require.NoError(t, err)
		require.Equal(t, 2, left, "4 and 5 arrived after what was seen")

		left, err = inbox.MarkAllRead(t.Context(), 1, 999)
		require.NoError(t, err)
		require.Zero(t, left)

		ivo, _ := inbox.Unread(t.Context(), 2)
		require.Equal(t, 1, ivo, "another person's inbox is untouched")
	})
}
