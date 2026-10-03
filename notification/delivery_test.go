package notification_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/identity/account"
	identitymail "github.com/wssto2/go-core/identity/mailtext"
	"github.com/wssto2/go-core/mail"
	"github.com/wssto2/go-core/notification"
	"github.com/wssto2/go-core/notification/mailtext"
	"gorm.io/gorm"
)

// zagreb is where the quiet-hours tests live: the zone of the application.
func zagreb(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Europe/Zagreb")
	require.NoError(t, err)

	return loc
}

// atNoon puts the world's clock in the middle of the day in Zagreb, outside quiet hours.
func atNoon(w *world, loc *time.Location) {
	w.clock.Set(time.Date(2026, 6, 10, 12, 0, 0, 0, loc))
}

// NOTIF-DELIVERY-001: an e-mail per recipient whose setting is on and who has an address, in the person's language, with the
// title as subject, the body, a link that opens the notification and a footer that says why and where to change it.
func TestAPublishedEventIsEmailedOnceInThePersonsLanguage(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db, notification.TimeZone(zagreb(t)))
		atNoon(w, zagreb(t))
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1, 2}, Actor: 0})
		require.NoError(t, w.drain())

		require.Len(t, w.inbox(1).Items, 1, "the in-app notification is made")
		require.Len(t, w.inbox(2).Items, 1)

		ds := w.deliveries()
		require.Len(t, ds, 2)
		require.Equal(t, "email", ds[0].Channel)
		require.Equal(t, "pending", ds[0].Status)
		require.Zero(t, ds[0].DeviceID, "e-mail has no device")
		require.Equal(t, "ana@example.test", *ds[0].Address)
		require.True(t, ds[0].ExpiresAt.Equal(w.clock.Now().Add(notification.DeliveryTTL)))
		require.Empty(t, w.sink.Sent(), "nothing is sent until the worker runs")

		require.Equal(t, 2, w.deliverDue())

		// Ana speaks Croatian.
		got := w.sink.To("ana@example.test")
		require.Len(t, got, 1)
		require.Equal(t, "Hi Ana Horvat (hr)", got[0].Subject)
		require.Contains(t, got[0].Text, "Poštovani Ana Horvat,")
		require.Contains(t, got[0].Text, "Otvori: https://tickets.example.test/tickets/7")
		require.Contains(t, got[0].Text, "postavkama obavijesti")
		require.Contains(t, got[0].HTML, `<a href="https://tickets.example.test/tickets/7">Otvori</a>`)

		// Ivo, English.
		got = w.sink.To("ivo@example.test")
		require.Len(t, got, 1)
		require.Contains(t, got[0].Text, "Hello Ivo,")
		require.Contains(t, got[0].Text, "Open: https://tickets.example.test/tickets/7")

		for _, d := range w.deliveries() {
			require.Equal(t, "sent", d.Status)
			require.Equal(t, uint32(1), d.Attempts)
			require.NotNil(t, d.SentAt)
		}

		require.Zero(t, w.deliverDue(), "sent once")
		require.Len(t, w.sink.Sent(), 2)
	})
}

// An e-mail goes to the people whose setting is on and who have an address: not an inactive account, not a person who turned it
// off, not a category that is in-app only unless they turn it on.
func TestOnlyThePeopleWhoseSettingIsOnAndWhoHaveAnAddressAreEmailed(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db, notification.TimeZone(zagreb(t)))
		atNoon(w, zagreb(t))
		w.consumeAssigned()
		w.consumeCommented()
		w.start()

		_, err := w.notices.Settings.SetEmail(t.Context(), 2, "tickets.assigned", false) // Ivo turned it off
		require.NoError(t, err)
		_, err = w.notices.Settings.SetEmail(t.Context(), 1, "tickets.commented", true) // Ana turned commented on
		require.NoError(t, err)

		// 1 Ana, 2 Ivo (assigned off), 3 Eva (no address), 4 Max (inactive).
		w.publish(assignedEvent{TicketID: 7, To: []int{1, 2, 3, 4}})
		publishOn(t, w.db, commented, assignedEvent{TicketID: 9, To: []int{1, 2, 3, 4}})
		require.NoError(t, w.drain())

		for _, person := range []int{1, 2, 3} {
			require.Len(t, w.inbox(person).Items, 2, "in-app is always made for the active")
		}

		require.Empty(t, w.inbox(4).Items)

		ds := w.deliveries()
		require.Len(t, ds, 2, "Ana, for assigned and for commented; nobody else")

		for _, d := range ds {
			require.Equal(t, "ana@example.test", *d.Address)
		}

		require.Equal(t, 2, w.deliverDue())
		require.Len(t, w.sink.Sent(), 2)
	})
}

// Without mail (identity.WithoutMail) there are no delivery rows and nothing is sent, whatever a person once chose.
func TestWithoutMailNoDeliveryIsMade(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, nil)
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1, 2}})
		require.NoError(t, w.drain())

		require.Len(t, w.inbox(1).Items, 1)
		require.Zero(t, w.count("notification_deliveries"))

		n, err := w.notices.DeliverDue(t.Context())
		require.NoError(t, err)
		require.Zero(t, n)
	})
}

// The enforcer is the first word: on for a person who turned it off, off for one who left it on.
func TestEnforcementDecidesWhoIsEmailed(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db, notification.TimeZone(zagreb(t)), notification.Enforce(forceAssigned)) // Ana on, Ivo off
		atNoon(w, zagreb(t))
		w.consumeAssigned()
		w.start()

		require.NoError(t, db.Table("notification_preferences").Create(map[string]any{"user_id": 1, "category": "tickets.assigned", "channel": "email", "enabled": false, "updated_at": w.clock.Now()}).Error)

		w.publish(assignedEvent{TicketID: 7, To: []int{1, 2}})
		require.NoError(t, w.drain())

		ds := w.deliveries()
		require.Len(t, ds, 1)
		require.Equal(t, "ana@example.test", *ds[0].Address, "Ana turned it off, but it is enforced on; Ivo is enforced off")
	})
}

// A retried event makes no second notification and no second e-mail: the dedupe holds for deliveries too (NOTIF-EVENT-001).
func TestARetriedEventMakesNoSecondEmail(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db, notification.TimeZone(zagreb(t)))
		atNoon(w, zagreb(t))

		failures := 1
		w.app.Events(assigned.To("notifications.ticket-assigned", func(ctx context.Context, e assignedEvent) error {
			err := w.notices.Send(ctx, TicketAssigned, notification.To(e.To...), func(notification.Recipient) notification.Message {
				return notification.Message{Title: "Assigned", Link: "/tickets/7"}
			})
			if err == nil && failures > 0 {
				failures--

				return errors.New("the app was cut off after the notifications were written")
			}

			return err
		}))
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1, 2}})
		require.Error(t, w.drain())
		require.Len(t, w.deliveries(), 2)

		w.clock.Advance(time.Minute)
		require.NoError(t, w.drain(), "the retry succeeds")

		require.Equal(t, int64(2), w.count("notifications"))
		require.Len(t, w.deliveries(), 2, "one delivery per person, not two")

		w.deliverDue()
		w.clock.Advance(time.Hour)
		w.deliverDue()
		require.Len(t, w.sink.Sent(), 2, "one e-mail per person")
	})
}

// NOTIF-QUIET-001: during quiet hours the in-app notification is made at once and the e-mail is held until they end; it is not
// sent a minute before, and then it is.
func TestAnEmailMadeInQuietHoursIsHeldUntilTheyEnd(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		loc := zagreb(t)
		w := newMailWorld(t, db, notification.TimeZone(loc))
		w.clock.Set(time.Date(2026, 6, 10, 22, 0, 0, 0, loc))
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1}})
		require.NoError(t, w.drain())

		require.Len(t, w.inbox(1).Items, 1, "the in-app notification is not held")

		release := time.Date(2026, 6, 11, 7, 0, 0, 0, loc)
		ds := w.deliveries()
		require.Len(t, ds, 1)
		require.Equal(t, "held", ds[0].Status)
		require.True(t, ds[0].NextAttemptAt.Equal(release))
		require.True(t, ds[0].ExpiresAt.Equal(release.Add(notification.DeliveryTTL)), "the TTL starts at the release, not at creation")

		require.Zero(t, w.deliverDue())

		w.clock.Set(release.Add(-time.Minute))
		require.Zero(t, w.deliverDue())
		require.Empty(t, w.sink.Sent())

		w.clock.Set(release)
		require.Equal(t, 1, w.deliverDue())
		require.Len(t, w.sink.Sent(), 1, "released when quiet hours end")
		require.Equal(t, "sent", w.deliveries()[0].Status)
	})
}

// A person's own quiet hours replace the default, in the application's zone; over midnight too; off holds nothing.
func TestAPersonsOwnQuietHoursDecideWhenTheirEmailIsHeld(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		loc := zagreb(t)
		w := newMailWorld(t, db, notification.TimeZone(loc))
		w.consumeAssigned()
		w.start()

		// Ana: 23:00-05:00 (over midnight). Ivo: off.
		_, err := w.notices.Settings.SetQuietHours(t.Context(), 1, notification.QuietHours{Enabled: true, Start: 23 * 60, End: 5 * 60})
		require.NoError(t, err)
		_, err = w.notices.Settings.SetQuietHours(t.Context(), 2, notification.QuietHours{Enabled: false, Start: 21 * 60, End: 7 * 60})
		require.NoError(t, err)

		w.clock.Set(time.Date(2026, 6, 10, 22, 0, 0, 0, loc)) // quiet by default, but not for Ana (23:00) and not for Ivo (off)
		w.publish(assignedEvent{TicketID: 7, To: []int{1, 2}})
		require.NoError(t, w.drain())
		require.Equal(t, 2, w.deliverDue())
		require.Len(t, w.sink.Sent(), 2)

		w.sink.Reset()
		w.clock.Set(time.Date(2026, 6, 10, 23, 30, 0, 0, loc)) // Ana's quiet hours; Ivo's are off
		w.publish(assignedEvent{TicketID: 8, To: []int{1, 2}})
		require.NoError(t, w.drain())
		require.Equal(t, 1, w.deliverDue())
		require.Len(t, w.sink.To("ivo@example.test"), 1)
		require.Empty(t, w.sink.To("ana@example.test"))

		w.clock.Set(time.Date(2026, 6, 11, 5, 0, 0, 0, loc))
		require.Equal(t, 1, w.deliverDue())
		require.Len(t, w.sink.To("ana@example.test"), 1)
	})
}

// NOTIF-QUIET-001, rule 3: a held e-mail goes out only if the notification is still unread; so does a pending one.
func TestAnEmailForANotificationReadBeforeItWasSentIsCancelled(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		loc := zagreb(t)
		w := newMailWorld(t, db, notification.TimeZone(loc))
		w.clock.Set(time.Date(2026, 6, 10, 22, 0, 0, 0, loc))
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1}})
		require.NoError(t, w.drain())

		_, err := w.notices.Inbox.MarkRead(t.Context(), 1, w.inbox(1).Items[0].ID)
		require.NoError(t, err)

		w.clock.Set(time.Date(2026, 6, 11, 7, 0, 0, 0, loc))
		require.Equal(t, 1, w.deliverDue())

		require.Empty(t, w.sink.Sent())

		d := w.deliveries()[0]
		require.Equal(t, "cancelled", d.Status)
		require.Equal(t, "read before delivery", *d.LastError)
	})
}

func TestAnEmailForANotificationThatWasDeletedIsCancelled(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db, notification.TimeZone(zagreb(t)))
		atNoon(w, zagreb(t))
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1}})
		require.NoError(t, w.drain())
		require.NoError(t, db.Exec("DELETE FROM notifications").Error)

		require.Equal(t, 1, w.deliverDue())
		require.Empty(t, w.sink.Sent())
		require.Equal(t, "cancelled", w.deliveries()[0].Status)
	})
}

// failing is a sender that always fails, counting how often it was asked.
type failing struct {
	mu    sync.Mutex
	err   error
	tries int
}

func (f *failing) Send(context.Context, mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.tries++

	return f.err
}

// NOTIF-DELIVERY-001, rule 3: retried with a backoff from 30 seconds, doubling to an hour, within the TTL of 24 hours; then failed.
func TestAFailingEmailIsRetriedWithBackoffThenFailedWhenTheTTLRunsOut(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		relay := &failing{err: errors.New("mail: cannot reach the relay")}
		w := newSenderWorld(t, db, relay, notification.TimeZone(zagreb(t)))
		atNoon(w, zagreb(t))
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1}})
		require.NoError(t, w.drain())

		created := w.clock.Now()
		expires := created.Add(notification.DeliveryTTL)

		var gaps []time.Duration

		for range 100 {
			d := w.deliveries()[0]
			if d.Status == "failed" {
				break
			}

			require.Equal(t, "pending", d.Status)
			require.True(t, d.NextAttemptAt.Before(expires), "no retry past the TTL")

			if d.Attempts > 0 {
				gaps = append(gaps, d.NextAttemptAt.Sub(w.clock.Now()))
			}

			w.clock.Set(d.NextAttemptAt)
			require.Equal(t, 1, w.deliverDue())
		}

		d := w.deliveries()[0]
		require.Equal(t, "failed", d.Status)
		require.Contains(t, *d.LastError, "expired: ")
		require.Contains(t, *d.LastError, "cannot reach the relay")
		require.Equal(t, int(d.Attempts), relay.tries)
		require.Nil(t, d.SentAt)

		require.Equal(t, []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour},
			gaps[:9], "30 s doubling, at most an hour")
		require.Less(t, d.UpdatedAt.Sub(created), notification.DeliveryTTL)

		require.Zero(t, w.deliverDue(), "a failed delivery is not tried again")
		require.Empty(t, w.sink.Sent())
	})
}

// A permanent mail error (a bad address, a recipient the relay refuses for good) fails at once, without a retry.
func TestAPermanentMailErrorFailsAtOnce(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		relay := &failing{err: fmt.Errorf("mail: the relay refused the recipient: %w", mail.ErrPermanent)}
		w := newSenderWorld(t, db, relay, notification.TimeZone(zagreb(t)))
		atNoon(w, zagreb(t))
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1}})
		require.NoError(t, w.drain())
		require.Equal(t, 1, w.deliverDue())

		d := w.deliveries()[0]
		require.Equal(t, "failed", d.Status)
		require.Equal(t, uint32(1), d.Attempts)
		require.Contains(t, *d.LastError, "permanent: ")

		w.clock.Advance(time.Hour)
		require.Zero(t, w.deliverDue())
		require.Equal(t, 1, relay.tries)
	})
}

// A delivery is sent once, however many workers run: a delivery claimed by one is leased, and another claiming meanwhile
// finds nothing (NOTIF-DELIVERY-001, rule 4).
func TestADeliveryBeingSentIsLeasedAndNotSentTwice(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		var w *world

		inner := 0
		relay := mail.SenderFunc(func(ctx context.Context, _ mail.Message) error {
			// While this worker is sending, a second worker looks for due deliveries.
			n, err := w.notices.DeliverDue(ctx)
			require.NoError(t, err)

			inner = n

			return nil
		})

		w = newSenderWorld(t, db, relay, notification.TimeZone(zagreb(t)))
		atNoon(w, zagreb(t))
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1}})
		require.NoError(t, w.drain())
		require.Equal(t, 1, w.deliverDue())

		require.Zero(t, inner, "the second worker found the claimed delivery leased")
		require.Equal(t, "sent", w.deliveries()[0].Status)
	})
}

// Workers racing on the same due deliveries send each one once (real servers: the locking read is what decides).
func TestWorkersRacingOnTheSameDeliveriesSendEachOnce(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db, notification.TimeZone(zagreb(t)))
		atNoon(w, zagreb(t))
		w.consumeAssigned()
		w.start()

		for i := range 20 {
			w.publish(assignedEvent{TicketID: i + 1, To: []int{1, 2}})
		}

		require.NoError(t, w.drain())
		require.Equal(t, int64(40), w.count("notification_deliveries"))

		var wg sync.WaitGroup

		for range 4 {
			wg.Add(1)

			go func() {
				defer wg.Done()

				_, err := w.notices.DeliverDue(context.Background())
				if err != nil && !strings.Contains(err.Error(), "Deadlock") {
					t.Errorf("DeliverDue: %v", err)
				}
			}()
		}

		wg.Wait()

		for left := 1; left > 0; { // what a deadlocked claim left
			left = w.deliverDue()
		}

		require.Len(t, w.sink.Sent(), 40, "each delivery once")
	}, dbtest.MySQL, dbtest.MariaDB)
}

// The application's own renderer, given to identity as WithMailContent, writes the notification mail too when it knows it.
func TestTheApplicationsRendererWritesTheMailWhenItKnowsIt(t *testing.T) {
	var seen mailtext.EmailData

	own := mail.RendererFunc(func(_ context.Context, locale string, name mail.Name, data any) (mail.Content, error) {
		if name != mailtext.Email {
			return mail.Content{}, mail.ErrNoTemplate
		}

		seen = data.(mailtext.EmailData)

		return mail.Content{Subject: "ARV - " + seen.Title, Text: "own look in " + locale}, nil
	})

	w := newRendererWorld(t, mustSQLite(t), own, notification.TimeZone(zagreb(t)))
	atNoon(w, zagreb(t))
	w.consumeAssigned()
	w.start()

	w.publish(assignedEvent{TicketID: 7, To: []int{1}})
	require.NoError(t, w.drain())
	require.Equal(t, 1, w.deliverDue())

	got := w.sink.Sent()
	require.Len(t, got, 1)
	require.Equal(t, "ARV - Hi Ana Horvat (hr)", got[0].Subject)
	require.Equal(t, "own look in hr", got[0].Text)
	require.Equal(t, mailtext.EmailData{Name: "Ana Horvat", Title: "Hi Ana Horvat (hr)", OpenURL: "https://tickets.example.test/tickets/7"}, seen)
}

// No link, no button; a title on several lines is one line in the subject; AppURL's path is kept.
func TestTheMailHasNoButtonWithoutALinkAndAOneLineSubject(t *testing.T) {
	sink := mail.NewSink()
	w := buildWorld(t, mustSQLite(t), nil, sink, account.Mail{Sender: sink, Renderer: identitymail.Defaults},
		notification.TimeZone(zagreb(t)), notification.AppURL("https://example.test/app/"))
	atNoon(w, zagreb(t))

	w.app.Events(assigned.To("notifications.ticket-assigned", func(ctx context.Context, e assignedEvent) error {
		return w.notices.Send(ctx, TicketAssigned, notification.To(e.To...), func(notification.Recipient) notification.Message {
			link := ""
			if e.TicketID == 2 {
				link = "/tickets/2"
			}

			return notification.Message{Title: "Two\nlines", Body: "Details", Link: link}
		})
	}))
	w.start()

	w.publish(assignedEvent{TicketID: 1, To: []int{2}})
	w.publish(assignedEvent{TicketID: 2, To: []int{2}})
	require.NoError(t, w.drain())
	require.Equal(t, 2, w.deliverDue())

	sent := w.sink.To("ivo@example.test")
	require.Len(t, sent, 2)
	require.Equal(t, "Two lines", sent[0].Subject)
	require.NotContains(t, sent[0].Text, "Open")
	require.NotContains(t, sent[0].HTML, "<a ")
	require.Contains(t, sent[1].Text, "Open: https://example.test/app/tickets/2")
}

// A worker starts no send in the last minute of its lease; what it left comes due again when the lease ends.
func TestNoSendStartsInTheLastMinuteOfTheLease(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		var w *world

		sent := 0
		relay := mail.SenderFunc(func(context.Context, mail.Message) error {
			sent++
			w.clock.Advance(4*time.Minute + 30*time.Second) // a slow relay

			return nil
		})

		w = newSenderWorld(t, db, relay, notification.TimeZone(zagreb(t)))
		atNoon(w, zagreb(t))
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1, 2}})
		require.NoError(t, w.drain())

		claimed := w.clock.Now()

		require.Equal(t, 2, w.deliverDue(), "both were claimed")
		require.Equal(t, 1, sent, "the second would start 30 s before its lease ends")

		left := w.deliveries()[1]
		require.Equal(t, "pending", left.Status)
		require.True(t, left.NextAttemptAt.Equal(claimed.Add(5*time.Minute)), "due again when the lease ends")

		require.Zero(t, w.deliverDue(), "still leased")

		w.clock.Set(claimed.Add(5*time.Minute + time.Second))
		require.Equal(t, 1, w.deliverDue())
		require.Equal(t, 2, sent)
	})
}
