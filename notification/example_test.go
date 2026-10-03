package notification_test

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/notification"
)

// exampleT lets an Example use the helpers that take a testing.TB.
type exampleT struct {
	testing.TB
	cleanups []func()
}

func (*exampleT) Helper()                      {}
func (t *exampleT) Cleanup(f func())           { t.cleanups = append(t.cleanups, f) }
func (*exampleT) Context() context.Context     { return context.Background() }
func (*exampleT) Log(...any)                   {}
func (*exampleT) Fatal(args ...any)            { log.Fatal(args...) }
func (*exampleT) Fatalf(f string, args ...any) { log.Fatalf(f, args...) }

func (t *exampleT) done() {
	for _, f := range t.cleanups {
		f()
	}
}

// The app declares its categories once, as values.
var TicketAssigned = notification.Category("tickets.assigned").EmailByDefault()

// TicketAssignedEvent is the fact a feature publishes: ids and facts, never text.
type TicketAssignedEvent struct {
	TicketID   int
	AssigneeID int
	ActorID    int
	Title      string
}

var Assigned = event.Define[TicketAssignedEvent]("tickets.assigned")

// newApp is a test application whose authorizer allows everything and whose catalogue has the dead-letter
// permissions, which the module's routes need to start.
func newApp(t testing.TB, opts ...gocoretest.Option) *gocore.App {
	t.Helper()

	cat := authz.NewCatalogue()
	if err := notification.DefinePermissions(cat); err != nil {
		t.Fatal(err)
	}

	app := gocoretest.New(t, append([]gocoretest.Option{gocoretest.Authorizer(authztest.AllowAll())}, opts...)...)
	app.Permissions(cat)

	return app
}

// people are the accounts of the examples: Ana (1) and Ivo (2) speak Croatian, Eva (3) English.
func people(t *exampleT) (*notification.Notices, *gocore.App) {
	ana, ivo, eva := identitytest.Account(1, "ana", "x"), identitytest.Account(2, "ivo", "x"), identitytest.Account(3, "eva", "x")
	ana.Locale, ivo.Locale = "hr", "hr"

	app := newApp(t)
	notices := notification.Install(app, identitytest.Users(t, ana, ivo, eva), TicketAssigned)

	return notices, app
}

// A feature turns its own event into notifications, in a consumer. Send is called
// once per event and renders once per recipient, in their language; the actor is
// left out.
func Example() {
	t := &exampleT{}
	defer t.done()

	notices, app := people(t)

	app.Events(Assigned.To("notifications.ticket-assigned",
		func(ctx context.Context, e TicketAssignedEvent) error {
			return notices.Send(ctx, TicketAssigned, notification.To(e.AssigneeID).Except(e.ActorID),
				func(r notification.Recipient) notification.Message {
					title := "You were assigned " + e.Title
					if r.Locale == "hr" {
						title = "Dodijeljen vam je " + e.Title
					}

					return notification.Message{Title: title, Link: fmt.Sprintf("/tickets/%d", e.TicketID)}
				})
		}))

	gocoretest.Publish(t, app, Assigned, TicketAssignedEvent{TicketID: 7, AssigneeID: 1, ActorID: 2, Title: "Login bug"})
	gocoretest.Publish(t, app, Assigned, TicketAssignedEvent{TicketID: 8, AssigneeID: 2, ActorID: 2, Title: "Own ticket"}) // the actor: nobody is told

	page, _ := notices.Inbox.List(context.Background(), 1, notification.ListQuery{})
	for _, n := range page.Items {
		fmt.Println(n.Category, n.Title, n.Link)
	}

	unread, _ := notices.Inbox.Unread(context.Background(), 2)
	fmt.Println("Ivo unread:", unread)
	// Output:
	// tickets.assigned Dodijeljen vam je Login bug /tickets/7
	// Ivo unread: 0
}

func ExampleCategory() {
	// Declared once, as a value, and passed to Install. In-app always; e-mail only if the person turns it on.
	var invoicePaid = notification.Category("billing.invoice-paid")

	fmt.Println(invoicePaid, notification.CategoryMax)
	// Output: billing.invoice-paid 64
}

func ExampleKind_EmailByDefault() {
	// E-mail is on until the person turns it off.
	var assigned = notification.Category("tickets.assigned").EmailByDefault()

	fmt.Println(assigned.Code())
	// Output: tickets.assigned
}

func ExampleAppURL() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	users := identitytest.Users(t, identitytest.Account(1, "ana", "x"))

	// The e-mail of a notification links to AppURL plus the message's in-app link.
	notification.Install(app, users, TicketAssigned, notification.AppURL("https://tickets.example.com"))

	_, err := app.Handler()
	fmt.Println(err)
	// Output: <nil>
}

func ExampleTimeZone() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	users := identitytest.Users(t, identitytest.Account(1, "ana", "x"))
	zagreb, _ := time.LoadLocation("Europe/Zagreb")

	// Quiet hours are read on the wall clock of this zone.
	notification.Install(app, users, TicketAssigned, notification.TimeZone(zagreb))

	_, err := app.Handler()
	fmt.Println(err)
	// Output: <nil>
}

func ExampleEnforce() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	users := identitytest.Users(t, identitytest.Account(1, "ana", "x"))

	// An outside authority, such as a dealer's policy: always on, and the person cannot change it.
	notification.Install(app, users, TicketAssigned,
		notification.Enforce(func(_ context.Context, _ int, k notification.Kind) (on, enforced bool, err error) {
			return true, k.Code() == "tickets.assigned", nil
		}))

	_, err := app.Handler()
	fmt.Println(err)
	// Output: <nil>
}

func ExampleInstall() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	users := identitytest.Users(t, identitytest.Account(1, "ana", "x"))

	notices := notification.Install(app, users, TicketAssigned)
	fmt.Println(notices.Inbox != nil)

	// A bad or repeated code is a start-up problem, listed with its fix by Check or Run.
	notification.Install(app, users, notification.Category("Bad Code"))

	_, err := app.Handler()
	fmt.Println(err != nil)
	// Output:
	// true
	// true
}

// Outside an event consumer Send fails and says what to do instead.
func ExampleNotices_Send() {
	t := &exampleT{}
	defer t.done()

	notices, _ := people(t)

	err := notices.Send(context.Background(), TicketAssigned, notification.To(1), func(notification.Recipient) notification.Message {
		return notification.Message{Title: "Hello"}
	})
	fmt.Println(err != nil)
	// Output: true
}

// To names the people; Except takes the actor out. Ids that are not valid and repeats are dropped
// when the notification is sent.
func ExampleTo() {
	recipients := notification.To(1, 2, 2, 0, -3).Except(2)

	_ = recipients

	fmt.Println("everyone but Ivo")
	// Output: everyone but Ivo
}

func ExampleRecipients_Except() {
	e := TicketAssignedEvent{AssigneeID: 3, ActorID: 3}

	// Assigning a ticket to yourself notifies nobody.
	_ = notification.To(e.AssigneeID).Except(e.ActorID)

	fmt.Println("nobody")
	// Output: nobody
}

// A link is a path inside the app: nothing can send a person off-site.
func ExampleIsAppLink() {
	for _, link := range []string{"/tickets/7", "", "https://evil.example", "//evil.example", `/\evil`} {
		fmt.Printf("%q %v\n", link, notification.IsAppLink(link))
	}
	// Output:
	// "/tickets/7" true
	// "" true
	// "https://evil.example" false
	// "//evil.example" false
	// "/\\evil" false
}

// The dedupe key is why an event delivered twice makes one notification.
func ExampleDedupeKey() {
	fmt.Println(notification.DedupeKey(1234, 56, TicketAssigned))
	// Output: 1234:56:tickets.assigned
}

// A person lists, counts and reads their own notifications. Reading one marks it read on
// every device; "mark all" marks what the person has seen, up to the newest id they show.
func ExampleInbox() {
	t := &exampleT{}
	defer t.done()

	notices, app := people(t)
	notify := notifier(t, notices, app)
	notify(1, "First")
	notify(1, "Second")

	ctx := context.Background()
	page, _ := notices.Inbox.List(ctx, 1, notification.ListQuery{Limit: 1})
	fmt.Println(page.Items[0].Title, page.HasMore)

	left, _ := notices.Inbox.MarkRead(ctx, 1, page.Items[0].ID)
	fmt.Println("unread", left)

	left, _ = notices.Inbox.MarkAllRead(ctx, 1, page.Items[0].ID)
	fmt.Println("unread", left)
	// Output:
	// Second true
	// unread 1
	// unread 0
}

type note struct {
	To    int
	Title string
}

// notifier returns a function that sends one notification to a person through the event queue, as a consumer would.
func notifier(t *exampleT, notices *notification.Notices, app *gocore.App) func(to int, title string) {
	ev := event.Define[note]("examples.notify")

	app.Events(ev.To("examples.notify", func(ctx context.Context, n note) error {
		return notices.Send(ctx, TicketAssigned, notification.To(n.To), func(notification.Recipient) notification.Message {
			return notification.Message{Title: n.Title}
		})
	}))

	return func(to int, title string) { gocoretest.Publish(t, app, ev, note{To: to, Title: title}) }
}

func ExampleMigrate() {
	t := &exampleT{}
	defer t.done()

	app := gocoretest.New(t)

	fmt.Println(notification.Migrate(app.Database()))
	// Output: <nil>
}

// Every app a person has open hears of a new notification once it is committed, and of what they read
// on another device.
func ExampleInbox_Subscribe() {
	t := &exampleT{}
	defer t.done()

	notices, app := people(t)
	notify := notifier(t, notices, app)

	ctx := context.Background()
	events, cancel := notices.Inbox.Subscribe(ctx, 1)

	defer cancel()

	notify(1, "Hello")

	created := <-events
	fmt.Println(created.Kind, created.Notification.Title, created.UnreadCount)

	_, _ = notices.Inbox.MarkRead(ctx, 1, created.Notification.ID)

	read := <-events
	fmt.Println(read.Kind, read.ReadIDs, read.UnreadCount)
	// Output:
	// created Hello 1
	// read [1] 0
}

// The hub is process-local: one instance only. It is what Inbox.Subscribe uses; a feature with its own
// kind of change publishes to it directly.
func ExampleHub() {
	hub := notification.NewHub()
	ctx := context.Background()

	events, cancel := hub.Subscribe(ctx, 7)
	defer cancel()

	hub.Publish(ctx, 7, notification.StreamEvent{Kind: notification.StreamUnread, UnreadCount: 3})
	hub.Publish(ctx, 8, notification.StreamEvent{Kind: notification.StreamUnread, UnreadCount: 9}) // nobody has 8 open

	fmt.Println((<-events).UnreadCount, hub.Subscribers(7), hub.Subscribers(8))
	// Output: 3 1 0
}

// The routes are declared as values: the TypeScript contract is generated from them without starting anything.
func ExampleRoutes() {
	for _, spec := range notification.Routes.Specs() {
		fmt.Println(spec.Method, spec.Path)
	}
	// Output:
	// GET /v1/notifications
	// GET /v1/notifications/unread
	// POST /v1/notifications/:id/read
	// POST /v1/notifications/read
	// GET /v1/notifications/stream
	// POST /v1/notifications/test
}

// A signed-in person sends themselves a test notification through the event queue and reads it over
// HTTP.
func ExampleDeclare() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t, gocoretest.SignedIn(authz.User(1, 0)))
	notification.Install(app, identitytest.Users(t, identitytest.Account(1, "ana", "x")))

	gocoretest.Do(t, app, "POST", "/v1/notifications/test", nil)
	_ = app.DrainEvents(context.Background()) // what the background worker does

	page := gocoretest.Decode[notification.Page](t, gocoretest.Do(t, app, "GET", "/v1/notifications", nil))
	fmt.Println(page.Items[0].Category, page.Items[0].Title)
	// Output: system.test Test notification
}

// The dead-letter permissions are defined on the application's catalogue, next to its own.
func ExampleDefinePermissions() {
	catalogue := authz.NewCatalogue()
	_ = notification.DefinePermissions(catalogue)

	_, view := catalogue.Lookup(notification.ViewDeadLetters)
	_, retry := catalogue.Lookup(notification.RetryDeadLetters)
	fmt.Println(notification.ViewDeadLetters, view, notification.RetryDeadLetters, retry)
	// Output: events.deadletter:view true events.deadletter:retry true
}

// Dead letters are generic over every consumer of the event queue, so the routes are not under /notifications.
func ExampleDeadLetterRoutes() {
	for _, spec := range notification.DeadLetterRoutes.Specs() {
		fmt.Println(spec.Method, spec.Path, spec.Permission)
	}
	// Output:
	// GET /v1/events/dead-letters events.deadletter:view
	// POST /v1/events/dead-letters/:event/:consumer/retry events.deadletter:retry
	// POST /v1/events/dead-letters/retry events.deadletter:retry
}

// A consumer that gave up on an event leaves a dead letter, listed and retried over HTTP by whoever holds
// the permissions: one letter, or all of one consumer's.
func ExampleDeclareDeadLetters() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t, gocoretest.SignedIn(authz.User(1, 0)))
	notification.Install(app, identitytest.Users(t, identitytest.Account(1, "ana", "x")))

	ev := event.Define[note]("examples.always-fails")
	app.Events(ev.To("examples.always-fails", func(context.Context, note) error {
		return fmt.Errorf("the mail server is away")
	}).Retry(event.Attempts(1)))

	ctx := context.Background()
	_ = database.NewTransactor(app.Database()).WithinTransaction(ctx, func(ctx context.Context) error {
		return ev.Publish(ctx, note{To: 1})
	})
	_ = app.DrainEvents(ctx) // fails once: given up on at once

	list := gocoretest.Decode[datatable.DatatableResult[notification.DeadLetterRow]](t, gocoretest.Do(t, app, "GET", "/v1/events/dead-letters?consumer=examples.always-fails", nil))
	fmt.Println(list.Total, list.Data[0].Consumer, list.Data[0].EventName, list.Data[0].Retryable)

	retried := gocoretest.Decode[notification.Retried](t, gocoretest.Do(t, app, "POST", "/v1/events/dead-letters/retry", notification.RetryAllInput{Consumer: "examples.always-fails"}))
	fmt.Println(retried.Retried)
	// Output:
	// 1 examples.always-fails examples.always-fails true
	// 1
}
