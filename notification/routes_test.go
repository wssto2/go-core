package notification_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/notification"
	"gorm.io/gorm"
)

// The inbox routes need a signed-in person and no permission, and each person sees only their own.
func TestTheInboxRoutesServeOnlyThePersonsOwnNotifications(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := seededAs(t, db, authztest.DenyAll()) // no permission is held: the inbox needs none

		for _, path := range []string{"/v1/notifications", "/v1/notifications/unread"} {
			require.Equal(t, http.StatusUnauthorized, w.do(0, "GET", path, nil).Code, path)
		}

		require.Equal(t, http.StatusUnauthorized, w.do(0, "POST", "/v1/notifications/1/read", nil).Code)

		page := data[notification.Page](t, w.do(1, "GET", "/v1/notifications?limit=2", nil))
		require.Equal(t, []int{5, 4}, ids(page))
		require.True(t, page.HasMore)

		page = data[notification.Page](t, w.do(1, "GET", "/v1/notifications?limit=2&before_id=4", nil))
		require.Equal(t, []int{3, 2}, ids(page))

		require.Equal(t, 5, data[notification.UnreadCount](t, w.do(1, "GET", "/v1/notifications/unread", nil)).UnreadCount)
		require.Equal(t, 1, data[notification.UnreadCount](t, w.do(2, "GET", "/v1/notifications/unread", nil)).UnreadCount)

		require.Equal(t, 4, data[notification.UnreadCount](t, w.do(1, "POST", "/v1/notifications/5/read", nil)).UnreadCount)
		require.Equal(t, http.StatusNotFound, w.do(1, "POST", "/v1/notifications/6/read", nil).Code, "Ivo's notification")
		require.Equal(t, 1, data[notification.UnreadCount](t, w.do(2, "GET", "/v1/notifications/unread", nil)).UnreadCount)

		require.Equal(t, 4, data[notification.UnreadCount](t, w.do(1, "POST", "/v1/notifications/read", notification.MarkAllInput{})).UnreadCount, "nothing seen: nothing marked")
		require.Equal(t, 1, data[notification.UnreadCount](t, w.do(1, "POST", "/v1/notifications/read", notification.MarkAllInput{UpToID: 3})).UnreadCount)
		require.Equal(t, http.StatusUnprocessableEntity, w.do(1, "POST", "/v1/notifications/read", notification.MarkAllInput{UpToID: -1}).Code)
	})
}

func TestTheTestNotificationTravelsThroughTheQueueToTheSignedInPerson(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, authztest.DenyAll()).start()

		stream, cancel := w.notices.Inbox.Subscribe(t.Context(), 2)
		defer cancel()

		require.Equal(t, http.StatusNoContent, w.do(2, "POST", "/v1/notifications/test", nil).Code)
		require.Empty(t, w.titles(2), "queued, not yet handled")

		require.NoError(t, w.drain()) // what the consumer's worker does

		page := w.inbox(2)
		require.Len(t, page.Items, 1)
		require.Equal(t, "system.test", page.Items[0].Category)
		require.Equal(t, "Test notification", page.Items[0].Title, "the actor is told: the point is to notify yourself")
		require.Equal(t, 1, next(t, stream).UnreadCount)

		require.Equal(t, http.StatusUnauthorized, w.do(0, "POST", "/v1/notifications/test", nil).Code)
	})
}

// failing makes a consumer that dies on its first delivery, and leaves its dead letter.
func (w *world) failingConsumer(name string) {
	w.app.Events(assigned.To(name, func(context.Context, assignedEvent) error {
		return errors.New("the mail server is away")
	}).Retry(event.Attempts(1)))
}

// Dead letters are generic over every consumer, behind events.deadletter:view and :retry.
func TestDeadLetterRoutesListAndRetryWithThePermissions(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, authztest.AllowAll())
		w.failingConsumer("billing.invoice-mail")
		w.failingConsumer("crm.lead-mail")
		w.start()

		w.publish(assignedEvent{TicketID: 1})
		w.publish(assignedEvent{TicketID: 2})
		require.Error(t, w.drain())

		list := data[datatable.DatatableResult[notification.DeadLetterRow]](t, w.do(1, "GET", "/v1/events/dead-letters", nil))
		require.EqualValues(t, 4, list.Total, "two events, two consumers: every consumer's letters")
		require.Equal(t, 1, list.Page)
		require.EqualValues(t, 2, list.Data[0].EventID, "newest first")
		require.Equal(t, "billing.invoice-mail", list.Data[0].Consumer, "then by consumer")
		require.Equal(t, "tickets.assigned", list.Data[0].EventName)
		require.Equal(t, "the mail server is away", list.Data[0].LastError)
		require.True(t, list.Data[0].Retryable)

		one := data[datatable.DatatableResult[notification.DeadLetterRow]](t, w.do(1, "GET", "/v1/events/dead-letters?consumer=billing.invoice-mail&per_page=1&page=2", nil))
		require.EqualValues(t, 2, one.Total)
		require.Len(t, one.Data, 1)
		require.Equal(t, 2, one.LastPage)

		retried := data[notification.Retried](t, w.do(1, "POST", "/v1/events/dead-letters/1/billing.invoice-mail/retry", nil))
		require.Equal(t, 1, retried.Retried)
		require.Equal(t, http.StatusNotFound, w.do(1, "POST", "/v1/events/dead-letters/1/billing.invoice-mail/retry", nil).Code, "retried already")
		require.Equal(t, http.StatusNotFound, w.do(1, "POST", "/v1/events/dead-letters/1/nobody/retry", nil).Code, "no such consumer")

		all := data[notification.Retried](t, w.do(1, "POST", "/v1/events/dead-letters/retry", notification.RetryAllInput{Consumer: "crm.lead-mail"}))
		require.Equal(t, 2, all.Retried)
		require.Equal(t, http.StatusUnprocessableEntity, w.do(1, "POST", "/v1/events/dead-letters/retry", notification.RetryAllInput{}).Code, "a consumer is required")

		list = data[datatable.DatatableResult[notification.DeadLetterRow]](t, w.do(1, "GET", "/v1/events/dead-letters", nil))
		require.EqualValues(t, 1, list.Total, "billing's second event is the one left")
	})
}

func TestDeadLetterRoutesNeedTheirPermissions(t *testing.T) {
	for _, tc := range []struct {
		name       string
		authorizer *authztest.Fake
		list       int
		retry      int
		retryAll   int
	}{
		{"nothing", authztest.DenyAll(), http.StatusForbidden, http.StatusForbidden, http.StatusForbidden},
		{"view", authztest.DenyAll().Allow(notification.ViewDeadLetters), http.StatusOK, http.StatusForbidden, http.StatusForbidden},
		{"retry", authztest.DenyAll().Allow(notification.RetryDeadLetters), http.StatusForbidden, http.StatusConflict, http.StatusOK}, // allowed: no such event; nothing to retry
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
				w := newWorld(t, db, tc.authorizer).start()

				require.Equal(t, tc.list, w.do(1, "GET", "/v1/events/dead-letters", nil).Code, "list")
				require.Equal(t, tc.retry, w.do(1, "POST", "/v1/events/dead-letters/1/x/retry", nil).Code, "retry")
				require.Equal(t, tc.retryAll, w.do(1, "POST", "/v1/events/dead-letters/retry", notification.RetryAllInput{Consumer: "x"}).Code, "retry all")
			})
		})
	}
}

// readEvents reads the server-sent events of a response into a channel, as the browser does.
func readEvents(t *testing.T, resp *http.Response) <-chan notification.StreamEvent {
	t.Helper()

	out := make(chan notification.StreamEvent, 16)

	go func() {
		defer close(out)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			payload, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}

			var ev notification.StreamEvent
			if json.Unmarshal([]byte(payload), &ev) == nil {
				out <- ev
			}
		}
	}()

	return out
}

func TestTheStreamRouteOpensWithTheUnreadCountAndThenFollowsTheInbox(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := seeded(t, db)
		srv := httptest.NewServer(w.handler)

		defer srv.Close()

		request := func(person string) *http.Response {
			req, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL+"/v1/notifications/stream", nil)
			require.NoError(t, err)

			if person != "" {
				req.Header.Set("X-Person", person)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			return resp
		}

		denied := request("")
		_ = denied.Body.Close()
		require.Equal(t, http.StatusUnauthorized, denied.StatusCode)

		resp := request("1")

		defer func() { _ = resp.Body.Close() }()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
		require.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))

		events := readEvents(t, resp)

		snapshot := next(t, events)
		require.Equal(t, notification.StreamUnread, snapshot.Kind)
		require.Equal(t, 5, snapshot.UnreadCount)

		w.publish(assignedEvent{TicketID: 11, To: []int{1}})
		require.NoError(t, w.drain())

		created := next(t, events)
		require.Equal(t, notification.StreamCreated, created.Kind)
		require.Equal(t, "/tickets/11", created.Notification.Link)
		require.Equal(t, 6, created.UnreadCount)

		require.Equal(t, http.StatusOK, w.do(1, "POST", "/v1/notifications/7/read", nil).Code)

		read := next(t, events)
		require.Equal(t, notification.StreamRead, read.Kind)
		require.Equal(t, []int{7}, read.ReadIDs)
		require.Equal(t, 5, read.UnreadCount)

		require.Equal(t, http.StatusOK, w.do(1, "POST", "/v1/notifications/read", notification.MarkAllInput{UpToID: 4}).Code)

		all := next(t, events)
		require.True(t, all.AllRead)
		require.Equal(t, 1, all.UnreadCount)

	})
}
