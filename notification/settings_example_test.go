package notification_test

import (
	"context"
	"fmt"
	"time"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/identity/mailtext"
	"github.com/wssto2/go-core/mail"
	"github.com/wssto2/go-core/notification"
)

// mailed is the example application with identity's mail going to a sink, so e-mail is available.
func mailed(t *exampleT) *notification.Notices {
	users := identitytest.Users(t, identitytest.Account(1, "ana", "x"))
	users.SetMail(account.Mail{Sender: mail.NewSink(), Renderer: mailtext.Defaults})

	app := newApp(t)

	return notification.Install(app, users, TicketAssigned, TicketCommented, notification.AppURL("https://tickets.example.com"))
}

// A person sees every category with what decides it: their own choice, or the category's default.
func ExampleSettings_Get() {
	t := &exampleT{}
	defer t.done()

	notices := mailed(t)

	prefs, _ := notices.Settings.Get(t.Context(), 1)
	fmt.Println("e-mail available:", prefs.EmailAvailable)

	for _, c := range prefs.Categories {
		fmt.Println(c.Category, c.Email.Enabled, c.Email.Source)
	}

	fmt.Printf("quiet hours %v %02d:%02d-%02d:%02d\n", prefs.QuietHours.Enabled,
		prefs.QuietHours.Start/60, prefs.QuietHours.Start%60, prefs.QuietHours.End/60, prefs.QuietHours.End%60)
	// Output:
	// e-mail available: true
	// tickets.assigned true default
	// tickets.commented false default
	// quiet hours true 21:00-07:00
}

// Turning e-mail off for a category the person gets by default is their choice; turning it back is the default again.
func ExampleSettings_SetEmail() {
	t := &exampleT{}
	defer t.done()

	notices := mailed(t)

	off, _ := notices.Settings.SetEmail(t.Context(), 1, "tickets.assigned", false)
	fmt.Println(off.Category, off.Email.Enabled, off.Email.Source)

	on, _ := notices.Settings.SetEmail(t.Context(), 1, "tickets.assigned", true)
	fmt.Println(on.Category, on.Email.Enabled, on.Email.Source)
	// Output:
	// tickets.assigned false person
	// tickets.assigned true default
}

// Quiet hours are minutes after midnight; they may run over midnight. A window with no length is refused.
func ExampleSettings_SetQuietHours() {
	t := &exampleT{}
	defer t.done()

	notices := mailed(t)

	saved, _ := notices.Settings.SetQuietHours(t.Context(), 1, notification.QuietHours{Enabled: true, Start: 22*60 + 30, End: 6 * 60})
	fmt.Println(saved.Start, saved.End)

	_, err := notices.Settings.SetQuietHours(t.Context(), 1, notification.QuietHours{Enabled: true, Start: 60, End: 60})
	fmt.Println(err != nil)
	// Output:
	// 1350 360
	// true
}

func ExampleDefaultQuietHours() {
	q := notification.DefaultQuietHours()

	fmt.Println(q.Enabled, q.Start, q.End)
	// Output: true 1260 420
}

// An e-mail made during quiet hours is held until they end, on the wall clock of the zone.
func ExampleQuietHours_ReleaseAt() {
	zagreb, _ := time.LoadLocation("Europe/Zagreb")
	q := notification.DefaultQuietHours()

	night := time.Date(2026, 10, 24, 22, 0, 0, 0, zagreb)
	fmt.Println(q.ReleaseAt(night, zagreb).Format("2006-01-02 15:04"))

	day := time.Date(2026, 10, 24, 12, 0, 0, 0, zagreb)
	fmt.Println(q.ReleaseAt(day, zagreb).Equal(day))
	// Output:
	// 2026-10-25 07:00
	// true
}

func ExampleSetting_Enforced() {
	fmt.Println(notification.Setting{Enabled: true, Source: notification.SourceEnforced}.Enforced())
	// Output: true
}

// A person reads and changes their own settings over HTTP: no permission, only a signed-in session. Unavailable
// e-mail, an enforced setting and invalid quiet hours answer 422 with a reason the client translates.
func ExampleSetEmailInput() {
	t := &exampleT{}
	defer t.done()

	users := identitytest.Users(t, identitytest.Account(1, "ana", "x"))
	users.SetMail(account.Mail{Sender: mail.NewSink(), Renderer: mailtext.Defaults})

	app := newApp(t, gocoretest.SignedIn(authz.User(1, 0)))
	notification.Install(app, users, TicketAssigned, TicketCommented, notification.AppURL("https://tickets.example.com"))

	off := false
	set := gocoretest.Decode[notification.CategorySettings](t, gocoretest.Do(t, app, "PUT", "/v1/notifications/preferences/tickets.assigned", notification.SetEmailInput{Email: &off}))
	fmt.Println(set.Category, set.Email.Enabled, set.Email.Source)

	prefs := gocoretest.Decode[notification.Preferences](t, gocoretest.Do(t, app, "GET", "/v1/notifications/preferences", nil))
	fmt.Println(prefs.EmailAvailable, len(prefs.Categories), prefs.Categories[0].Email.Source)

	bad := gocoretest.Do(t, app, "PUT", "/v1/notifications/quiet-hours", notification.QuietHoursInput{Enabled: true, Start: 60, End: 60})
	fmt.Println(bad.Code)
	// Output:
	// tickets.assigned false person
	// true 2 person
	// 422
}

func ExampleConsumerNames() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t, gocoretest.SignedIn(authz.User(1, 0)))
	notification.Install(app, identitytest.Users(t, identitytest.Account(1, "ana", "x")))

	app.Events(Assigned.To("tickets.audit", func(context.Context, TicketAssignedEvent) error { return nil }))

	names := gocoretest.Decode[notification.ConsumerNames](t, gocoretest.Do(t, app, "GET", "/v1/events/consumers", nil))
	fmt.Println(names.Consumers)
	// Output: [notification.test tickets.audit]
}
