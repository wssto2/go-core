package notification_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/notification"
	"gorm.io/gorm"
)

type attempt struct {
	Attempts  int
	LastError string
	DeadAt    *time.Time
	DoneAt    *time.Time
}

func attemptOf(t *testing.T, w *world, eventID int, consumer string) attempt {
	t.Helper()

	var a attempt
	require.NoError(t, w.db.Table("event_consumer_attempts").Where("event_id = ? AND consumer = ?", eventID, consumer).Take(&a).Error)

	return a
}

// NOTIF-EVENT-001: a consumer sends, in the language of each person, and the actor is not told.
func TestAnEventBecomesANotificationInEachRecipientsLanguage(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1, 2, 3}, Actor: 2})
		require.NoError(t, w.drain())

		require.Equal(t, []string{"Hi Ana Horvat (hr)"}, w.titles(1))
		require.Empty(t, w.titles(2), "the actor is not told of their own action")
		require.Equal(t, []string{"Hi eva (en)"}, w.titles(3), "a person without a name shows their login")

		n := w.inbox(1).Items[0]
		require.Equal(t, TicketAssigned.Code(), n.Category)
		require.Equal(t, "/tickets/7", n.Link)
		require.Nil(t, n.ReadAt)
		require.Equal(t, w.clock.Now(), n.CreatedAt)
	})
}

// NOTIF-EVENT-001 rule 4: an event handled twice makes one notification per person.
func TestAnEventRetriedAfterItsNotificationsWereWrittenMakesNoSecondNotification(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)

		failures := 1
		w.app.Events(assigned.To("notifications.ticket-assigned", func(ctx context.Context, e assignedEvent) error {
			err := w.notices.Send(ctx, TicketAssigned, notification.To(e.To...), func(notification.Recipient) notification.Message {
				return notification.Message{Title: "Assigned"}
			})
			if err == nil && failures > 0 {
				failures--

				return errors.New("the app was cut off after the notifications were written") // delivered, not acknowledged
			}

			return err
		}))
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1, 2}})
		require.Error(t, w.drain())
		require.Equal(t, int64(2), w.count("notifications"))

		w.clock.Advance(time.Minute)
		require.NoError(t, w.drain(), "the retry succeeds")

		require.Equal(t, int64(2), w.count("notifications"), "one notification per person, not two")
		require.Len(t, w.inbox(1).Items, 1)
		require.NotNil(t, attemptOf(t, w, 1, "notifications.ticket-assigned").DoneAt)
	})
}

// The key is the event's id, the person and the category.
func TestTheDedupeKeyIsTheEventTheRecipientAndTheCategory(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1}})
		w.publish(assignedEvent{TicketID: 7, To: []int{1}}) // a second event is a second notification
		require.NoError(t, w.drain())

		var keys []string
		require.NoError(t, w.db.Table("notifications").Order("id").Pluck("dedupe_key", &keys).Error)
		require.Equal(t, []string{"1:1:tickets.assigned", "2:1:tickets.assigned"}, keys)
		require.Equal(t, notification.DedupeKey(1, 1, TicketAssigned), keys[0])
	})
}

// NOTIF-EVENT-001 rule 5: a failure is retried, with the queue's backoff, and after its attempts the event is a dead letter.
func TestAFailingSendIsRetriedUpToTheLimitThenDeadLettered(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)
		w.consumeAssigned(event.Attempts(3), event.Backoff(time.Minute, time.Hour))
		w.start()

		w.people.FailNext(100) // the directory is away

		w.publish(assignedEvent{TicketID: 7, To: []int{1}})

		require.ErrorContains(t, w.drain(), "the directory is away")
		require.Equal(t, 1, attemptOf(t, w, 1, "notifications.ticket-assigned").Attempts)
		require.NoError(t, w.drain(), "not due again yet")

		w.clock.Advance(time.Minute)
		require.Error(t, w.drain())
		require.Equal(t, 2, attemptOf(t, w, 1, "notifications.ticket-assigned").Attempts)

		w.clock.Advance(2 * time.Minute)
		require.Error(t, w.drain())

		a := attemptOf(t, w, 1, "notifications.ticket-assigned")
		require.Equal(t, 3, a.Attempts)
		require.NotNil(t, a.DeadAt, "after its attempts the event is a dead letter")
		require.Contains(t, a.LastError, "the directory is away")
		require.Zero(t, w.count("notifications"))

		// Put back in the queue once the directory is back, it is handled and the dead letter is gone.
		w.people.FailNext(0)
		require.NoError(t, event.NewDeadLetters(w.db, w.clock).Retry(t.Context(), 1, "notifications.ticket-assigned"))
		require.NoError(t, w.drain())
		require.Equal(t, []string{"Hi Ana Horvat (hr)"}, w.titles(1))
	})
}

// NOTIF-EVENT-001 rule 5: what can never be handled does not wait for retries.
func TestAnEventThatCanNeverBeHandledIsADeadLetterAtOnce(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)
		w.consumeAssigned()
		w.start()

		// An unreadable payload: the queue's own malformed event.
		require.NoError(t, w.db.Exec(
			`INSERT INTO outbox_events (event_type, envelope, created_at) VALUES ('tickets.assigned', '{"version":"1","payload":"not an object"}', ?)`, w.clock.Now()).Error)
		require.Error(t, w.drain())

		a := attemptOf(t, w, 1, "notifications.ticket-assigned")
		require.NotNil(t, a.DeadAt)
		require.Equal(t, 1, a.Attempts)
		require.Contains(t, a.LastError, "malformed")
		require.Zero(t, w.count("notifications"))
	})
}

func TestACategoryThatWasNotRegisteredIsADeadLetterWithTheFix(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)
		w.app.Events(assigned.To("notifications.ticket-assigned", func(ctx context.Context, _ assignedEvent) error {
			return w.notices.Send(ctx, notification.Category("tickets.forgotten"), notification.To(1), func(notification.Recipient) notification.Message {
				return notification.Message{Title: "x"}
			})
		}))
		w.start()

		w.publish(assignedEvent{To: []int{1}})
		err := w.drain()
		require.ErrorIs(t, err, notification.ErrUnknownCategory)
		require.ErrorIs(t, err, event.ErrMalformed)
		require.ErrorContains(t, err, "notification.Install")

		require.NotNil(t, attemptOf(t, w, 1, "notifications.ticket-assigned").DeadAt)
	})
}

// Send is for the consumer of an event: it needs the event's id to dedupe on, and says how to do it.
func TestSendOutsideAConsumerFailsAndSaysWhatToDo(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil).start()

		err := w.notices.Send(t.Context(), TicketAssigned, notification.To(1), func(notification.Recipient) notification.Message {
			return notification.Message{Title: "x"}
		})
		require.ErrorIs(t, err, notification.ErrNotInConsumer)
		require.ErrorContains(t, err, "publish an event and send from its consumer")
		require.Zero(t, w.count("notifications"))
	})
}

// NOTIF-RECIPIENT-001.
func TestRecipientsAreActiveDistinctValidAndNotTheActor(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)

		var seen []notification.Recipient

		w.app.Events(assigned.To("notifications.ticket-assigned", func(ctx context.Context, e assignedEvent) error {
			return w.notices.Send(ctx, TicketAssigned, notification.To(e.To...).Except(e.Actor),
				func(r notification.Recipient) notification.Message {
					seen = append(seen, r)

					return notification.Message{Title: "t"}
				})
		}))
		w.start()

		// Ana twice, Ivo (the actor), Max (inactive), 9 (no such person), 0 and -1 (not ids), then Eva.
		w.publish(assignedEvent{To: []int{1, 1, 2, 4, 9, 0, -1, 3, 1}, Actor: 2})
		require.NoError(t, w.drain())

		require.Equal(t, []notification.Recipient{{ID: 1, Name: "Ana Horvat", Locale: "hr"}, {ID: 3, Name: "eva", Locale: "en"}}, seen,
			"rendered once per recipient, in the order named")
		require.Equal(t, 1, w.people.Finds(), "one lookup per Send, whoever is named")

		for _, id := range []int{2, 4, 9} {
			require.Empty(t, w.titles(id), id)
		}

		require.Len(t, w.inbox(1).Items, 1, "Ana was named three times")
	})
}

func TestNobodyToNotifyLooksNobodyUp(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{To: []int{2}, Actor: 2}) // assigned to oneself
		require.NoError(t, w.drain())
		require.Zero(t, w.people.Finds())
		require.Zero(t, w.count("notifications"))
	})
}

// The actor is told when the application does not name them: the module's own test notification.
func TestExceptIsTheCallersChoice(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)
		w.app.Events(assigned.To("notifications.ticket-assigned", func(ctx context.Context, e assignedEvent) error {
			return w.notices.Send(ctx, TicketAssigned, notification.To(e.To...), func(notification.Recipient) notification.Message {
				return notification.Message{Title: "t"}
			})
		}))
		w.start()

		w.publish(assignedEvent{To: []int{2}, Actor: 2})
		require.NoError(t, w.drain())
		require.Len(t, w.inbox(2).Items, 1)
	})
}

// NOTIF-CONTENT-001.
func TestContentIsCutToItsColumnsAndTheTitleIsRequired(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)

		msg := notification.Message{Title: "  " + strings.Repeat("č", 200) + "  ", Body: strings.Repeat("b", 600), Link: " /tickets/7 ", Data: map[string]string{"ticket": "7"}}
		w.app.Events(assigned.To("notifications.ticket-assigned", func(ctx context.Context, e assignedEvent) error {
			return w.notices.Send(ctx, TicketAssigned, notification.To(e.To...), func(notification.Recipient) notification.Message { return msg })
		}))
		w.start()

		w.publish(assignedEvent{To: []int{1}})
		require.NoError(t, w.drain())

		n := w.inbox(1).Items[0]
		require.Equal(t, strings.Repeat("č", notification.TitleMax-1)+"…", n.Title, "cut to fit, ending in an ellipsis")
		require.Len(t, []rune(n.Body), notification.BodyMax)
		require.Equal(t, "/tickets/7", n.Link)
		require.Equal(t, map[string]string{"ticket": "7"}, n.Data)

		msg = notification.Message{Title: "  "}
		w.publish(assignedEvent{To: []int{2}})
		require.ErrorIs(t, w.drain(), notification.ErrInvalidMessage)
		require.Empty(t, w.titles(2))
	})
}

// A link is a path inside the app. One bad message among the recipients' fails the whole event, so nobody
// gets a notification the others did not: it is a dead letter at once, whatever is retried.
func TestALinkThatLeavesTheAppIsADeadLetterAndNobodyIsNotified(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)

		link := ""
		w.app.Events(assigned.To("notifications.ticket-assigned", func(ctx context.Context, e assignedEvent) error {
			return w.notices.Send(ctx, TicketAssigned, notification.To(e.To...), func(r notification.Recipient) notification.Message {
				if r.ID == 2 {
					return notification.Message{Title: "t", Link: link} // the second recipient's message is the bad one
				}

				return notification.Message{Title: "t", Link: "/ok"}
			})
		}))
		w.start()

		for i, bad := range []string{"https://evil.example/x", "//evil.example", `/\evil`, "tickets/7", "/a\nb", "/" + strings.Repeat("x", 255)} {
			link = bad

			w.publish(assignedEvent{To: []int{1, 2}})
			err := w.drain()
			require.ErrorIs(t, err, notification.ErrInvalidMessage, bad)
			require.ErrorIs(t, err, event.ErrMalformed, bad)
			require.NotNil(t, attemptOf(t, w, i+1, "notifications.ticket-assigned").DeadAt, bad)
		}

		require.Zero(t, w.count("notifications"), "Ana's valid message was not written either")

		link = "/tickets/7"

		w.publish(assignedEvent{To: []int{1, 2}})
		require.NoError(t, w.drain())
		require.Equal(t, int64(2), w.count("notifications"))
	})
}

func TestInstallListsWhatIsWrongWithTheCategoriesAndTheFix(t *testing.T) {
	for _, tc := range []struct {
		name string
		cats []notification.Option
		want string
	}{
		{"uppercase", []notification.Option{notification.Category("Tickets.Assigned")}, "lower-case words"},
		{"empty", []notification.Option{notification.Category("")}, "lower-case words"},
		{"too long", []notification.Option{notification.Category(strings.Repeat("a", 65))}, "at most 64"},
		{"twice", []notification.Option{TicketAssigned, TicketAssigned}, "registered twice"},
		{"the module's own", []notification.Option{notification.Category("system.test")}, "the module's own"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, mustSQLite(t), nil)
			notification.Install(w.app, w.people, tc.cats...)

			_, err := w.app.Handler()
			require.ErrorContains(t, err, tc.want)
			require.ErrorContains(t, err, "Fix:")
		})
	}

	w := newWorld(t, mustSQLite(t), nil)
	notification.Install(w.app, nil)

	_, err := w.app.Handler()
	require.ErrorContains(t, err, "notification needs the people to notify")
}

func TestInstallListsWhatIsWrongWithTheOptionsAndTheFix(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []notification.Option
		want string
	}{
		{"url without scheme", []notification.Option{notification.AppURL("tickets.example.com")}, "is not the address"},
		{"url with a query", []notification.Option{notification.AppURL("https://tickets.example.com/?a=b")}, "is not the address"},
		{"url twice", []notification.Option{notification.AppURL("https://a.example.com"), notification.AppURL("https://b.example.com")}, "AppURL was given twice"},
		{"nil zone", []notification.Option{notification.TimeZone(nil)}, "TimeZone was given no location"},
		{"zone twice", []notification.Option{notification.TimeZone(time.UTC), notification.TimeZone(time.UTC)}, "TimeZone was given twice"},
		{"nil enforcer", []notification.Option{notification.Enforce(nil)}, "Enforce was given no function"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, mustSQLite(t), nil)
			notification.Install(w.app, w.people, tc.opts...)

			_, err := w.app.Handler()
			require.ErrorContains(t, err, tc.want)
			require.ErrorContains(t, err, "Fix:")
		})
	}
}

func mustSQLite(t *testing.T) *gorm.DB {
	t.Helper()

	db, ok := dbtest.Open(t, dbtest.SQLite)
	require.True(t, ok)

	return db
}

// A feature writes and publishes in one transaction; its consumer sends; the person reads it over HTTP.
func TestAFeaturePublishesItsConsumerSendsAndThePersonReadsItOverHTTP(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)
		w.consumeAssigned()
		w.start()

		require.NoError(t, db.Exec("CREATE TABLE assignments (ticket_id INT, user_id INT)").Error)

		assign := func(ticket, user, actor int, commit bool) error {
			return database.NewTransactor(db).WithinTransaction(t.Context(), func(ctx context.Context) error {
				tx, _ := database.TxFromContext(ctx)
				if err := tx.Exec("INSERT INTO assignments VALUES (?, ?)", ticket, user).Error; err != nil {
					return err
				}

				if err := assigned.Publish(ctx, assignedEvent{TicketID: ticket, To: []int{user}, Actor: actor}); err != nil {
					return err
				}

				if !commit {
					return errors.New("the write failed after the event was queued")
				}

				return nil
			})
		}

		require.Error(t, assign(8, 1, 2, false))
		require.NoError(t, assign(7, 1, 2, true))
		require.NoError(t, w.drain())

		page := data[notification.Page](t, w.do(1, "GET", "/v1/notifications", nil))
		require.Len(t, page.Items, 1, "a write that rolled back never notifies")
		require.Equal(t, "Hi Ana Horvat (hr)", page.Items[0].Title)
		require.Equal(t, "/tickets/7", page.Items[0].Link)
		require.Nil(t, page.Items[0].ReadAt)

		require.Equal(t, 1, data[notification.UnreadCount](t, w.do(1, "GET", "/v1/notifications/unread", nil)).UnreadCount)
		require.Equal(t, 0, data[notification.UnreadCount](t, w.do(1, "POST", "/v1/notifications/1/read", nil)).UnreadCount)
		require.Zero(t, data[notification.UnreadCount](t, w.do(1, "GET", "/v1/notifications/unread", nil)).UnreadCount)
	})
}
