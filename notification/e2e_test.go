package notification_test

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/gormstore"
	"github.com/wssto2/go-core/mail"
	"github.com/wssto2/go-core/notification"
)

// freePort is a port nothing listens on: below the ephemeral range, where only another server could take it.
func freePort(t *testing.T) int {
	t.Helper()

	for range 50 {
		port := 20000 + rand.IntN(10000) //nolint:gosec // a test port, not a secret

		l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}

		_ = l.Close()

		return port
	}

	t.Fatal("no free port")

	return 0
}

// The whole path, with the real identity and a running application: a feature publishes an event, the person gets the in-app
// notification and one e-mail, and during their quiet hours the e-mail waits until they end. The clock
// is a fake the test moves; the workers Install started do the rest. Housekeeping runs at start and deletes what is old.
func TestEndToEndAFeaturePublishesAndThePersonIsNotifiedAndMailedOutsideQuietHours(t *testing.T) {
	loc := zagreb(t)
	clock := &fakeClock{now: time.Date(2026, 6, 10, 12, 0, 0, 0, loc)}

	cfg := bootstrap.DefaultConfig()
	cfg.HTTP.Port = freePort(t)
	cfg.HTTP.ShutdownTimeout = 5 * time.Second
	cfg.Frontend.StaticPath = ""

	reg, cleanup := database.NewTestRegistry("local")
	t.Cleanup(func() { _ = cleanup() })

	app := gocore.New(cfg, gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithClock(clock), gocore.WithAutoMigrate(t.Context()), gocore.WithAuthorizer(authztest.AllowAll()))

	catalogue := authz.NewCatalogue()
	require.NoError(t, identity.DefinePermissions(catalogue))
	require.NoError(t, notification.DefinePermissions(catalogue))
	app.Permissions(catalogue)

	sink := mail.NewSink()
	users := identity.Install(app, identity.WithMail(sink), identity.WithCodeSecret("an-installation-secret-of-32-chars!"))
	notices := notification.Install(app, users, TicketAssigned, notification.AppURL("https://tickets.example.test"), notification.TimeZone(loc))

	app.Events(assigned.To("notifications.ticket-assigned", func(ctx context.Context, e assignedEvent) error {
		return notices.Send(ctx, TicketAssigned, notification.To(e.To...).Except(e.Actor), func(r notification.Recipient) notification.Message {
			return notification.Message{Title: "Ticket " + strconv.Itoa(e.TicketID) + " for " + r.Name, Link: "/tickets/" + strconv.Itoa(e.TicketID)}
		})
	}))

	hash, err := account.Bcrypt{}.Hash("secret-password")
	require.NoError(t, err)

	_, err = gormstore.New(app.Database()).Accounts.Create(t.Context(), account.Account{
		ID: 1, Login: "ana", Name: "Ana", Email: "ana@example.test", Locale: "en", Active: true, PasswordHash: hash, CreatedAt: clock.Now(),
	})
	require.NoError(t, err)

	// A notification from before the retention period: the housekeeping that starts with the application deletes it.
	require.NoError(t, app.Database().Table("notifications").Create(map[string]any{
		"user_id": 1, "category": "tickets.assigned", "title": "Ancient", "body": "", "link": "", "data": "{}", "dedupe_key": "old:1", "created_at": clock.Now().Add(-100 * day),
	}).Error)

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- app.RunContext(ctx) }()

	t.Cleanup(func() {
		stop()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Error("the application did not stop")
		}
	})

	publish := func(ticket int) {
		require.NoError(t, database.NewTransactor(app.Database()).WithinTransaction(context.Background(), func(ctx context.Context) error {
			return assigned.Publish(ctx, assignedEvent{TicketID: ticket, To: []int{1}, Actor: 2})
		}))
	}

	unread := func() int {
		n, err := notices.Inbox.Unread(context.Background(), 1)
		require.NoError(t, err)

		return n
	}

	// Noon: the in-app notification, and one e-mail with a link to the application.
	publish(7)
	require.Eventually(t, func() bool { return unread() == 1 && len(sink.To("ana@example.test")) == 1 }, 10*time.Second, 50*time.Millisecond)

	got := sink.To("ana@example.test")[0]
	require.Equal(t, "Ticket 7 for Ana", got.Subject)
	require.Contains(t, got.Text, "Open: https://tickets.example.test/tickets/7")

	require.Eventually(t, func() bool {
		var n int64
		_ = app.Database().Table("notifications").Where("title = ?", "Ancient").Count(&n).Error

		return n == 0
	}, 5*time.Second, 50*time.Millisecond, "housekeeping deleted the old notification")

	// 22:00, quiet hours: the in-app notification at once, the e-mail not before 07:00.
	clock.Set(time.Date(2026, 6, 10, 22, 0, 0, 0, loc))
	publish(8)
	require.Eventually(t, func() bool { return unread() == 2 }, 10*time.Second, 50*time.Millisecond)
	require.Never(t, func() bool { return len(sink.To("ana@example.test")) > 1 }, 3*time.Second, 100*time.Millisecond, "held during quiet hours")

	clock.Set(time.Date(2026, 6, 11, 7, 0, 0, 0, loc))
	require.Eventually(t, func() bool { return len(sink.To("ana@example.test")) == 2 }, 10*time.Second, 50*time.Millisecond, "released when they end")
	require.Equal(t, "Ticket 8 for Ana", sink.To("ana@example.test")[1].Subject)

	require.Never(t, func() bool { return len(sink.Sent()) != 2 }, 2*time.Second, 100*time.Millisecond, "one e-mail per notification, no more")
}
