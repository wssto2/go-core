package notification

import (
	"context"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/route"
)

// UnreadCount is what every read answers with, so the badge updates without a second request.
type UnreadCount struct {
	UnreadCount int `json:"unread_count"`
}

// ReadInput addresses one notification of the signed-in person.
type ReadInput struct {
	ID int `path:"id"`
}

// MarkAllInput is "mark all as read". UpToID is the newest notification the person has seen, so one that
// arrives while they click is not marked read unseen; 0 (nothing seen) marks nothing (NOTIF-READ-001).
type MarkAllInput struct {
	UpToID int `json:"up_to_id" validation:"min:0"`
}

// base is the version the routes are declared under. A v2 is new declarations
// next to these, in a group of their own.
const base = "/v1"

// Routes is the declared HTTP contract of the notification module (paths under /v1, before the
// application's gocore.WithPrefix): what the TypeScript generator reads, without installing anything.
// Every route needs a signed-in person and no permission, and acts on that person's own inbox: the id
// comes from the session, never from the request.
var Routes = Declare().Contract()

// Declared is the routes of the module as values.
type Declared struct {
	// List is a page of the inbox, newest first; before_id loads the next.
	List route.Route[ListQuery, Page]
	// Unread is how many notifications are unread.
	Unread route.Route[route.None, UnreadCount]
	// MarkRead marks one notification read on every device.
	MarkRead route.Route[ReadInput, UnreadCount]
	// MarkAllRead marks the notifications up to up_to_id read.
	MarkAllRead route.Route[MarkAllInput, UnreadCount]
	// Stream is the live stream (server-sent events), a raw route.
	Stream route.RawRoute
	// Test sends the signed-in person a test notification through the event queue.
	Test route.Route[route.None, route.Empty]

	group *route.Contract
}

// Declare declares the routes.
func Declare() *Declared {
	inbox := base + "/notifications"

	d := &Declared{
		List:        route.Get[ListQuery, Page](inbox).Name("notification.list"),
		Unread:      route.Get[route.None, UnreadCount](inbox + "/unread").Name("notification.unread"),
		MarkRead:    route.Post[ReadInput, UnreadCount](inbox + "/:id/read").Name("notification.read"),
		MarkAllRead: route.Post[MarkAllInput, UnreadCount](inbox + "/read").Name("notification.read-all"),
		Stream:      route.Raw("GET", inbox+"/stream").Name("notification.stream"),
		Test:        route.Post[route.None, route.Empty](inbox + "/test").Name("notification.test"),
	}

	d.group = route.Group("notification", d.List, d.Unread, d.MarkRead, d.MarkAllRead, d.Stream, d.Test)

	return d
}

// Contract is the routes as a group: what contract.Generate reads.
func (d *Declared) Contract() *route.Contract { return d.group }

// To binds a handler to every route; hand the result to app.Routes.
func (d *Declared) To(n *Notices) []route.Handled {
	return []route.Handled{
		d.List.To(n.list),
		d.Unread.To(n.unread),
		d.MarkRead.To(n.markRead),
		d.MarkAllRead.To(n.markAll),
		d.Stream.To(n.stream(heartbeatInterval)),
		d.Test.To(n.sendTest),
	}
}

func (n *Notices) list(ctx context.Context, q ListQuery) (Page, error) {
	id, err := person(ctx)
	if err != nil {
		return Page{}, err
	}

	return n.Inbox.List(ctx, id, q)
}

func (n *Notices) unread(ctx context.Context, _ route.None) (UnreadCount, error) {
	id, err := person(ctx)
	if err != nil {
		return UnreadCount{}, err
	}

	count, err := n.Inbox.Unread(ctx, id)

	return UnreadCount{UnreadCount: count}, err
}

func (n *Notices) markRead(ctx context.Context, in ReadInput) (UnreadCount, error) {
	id, err := person(ctx)
	if err != nil {
		return UnreadCount{}, err
	}

	count, err := n.Inbox.MarkRead(ctx, id, in.ID)

	return UnreadCount{UnreadCount: count}, err
}

func (n *Notices) markAll(ctx context.Context, in MarkAllInput) (UnreadCount, error) {
	id, err := person(ctx)
	if err != nil {
		return UnreadCount{}, err
	}

	count, err := n.Inbox.MarkAllRead(ctx, id, in.UpToID)

	return UnreadCount{UnreadCount: count}, err
}

// testPayload is the module's own event: who asked for the test notification.
type testPayload struct {
	UserID int `json:"user_id"`
}

// tested is the module's own event, published by the test route. It shows a person that notifications
// reach them, through the whole path a feature's event takes: queue, consumer, inbox, stream.
var tested = event.Define[testPayload]("notification.test")

// testConsumer sends the test notification to the person who asked, the actor included: the point of it
// is to notify yourself.
func (n *Notices) testConsumer() event.Consumer {
	return tested.To("notification.test", func(ctx context.Context, p testPayload) error {
		return n.Send(ctx, systemTest, To(p.UserID), func(Recipient) Message {
			return Message{Title: "Test notification", Body: "If you see this, notifications reach this app."}
		})
	})
}

// sendTest queues the test event in a transaction of its own; the consumer makes the notification.
func (n *Notices) sendTest(ctx context.Context, _ route.None) (route.Empty, error) {
	id, err := person(ctx)
	if err != nil {
		return route.Empty{}, err
	}

	err = database.NewTransactor(n.db).WithinTransaction(ctx, func(ctx context.Context) error {
		return tested.Publish(ctx, testPayload{UserID: id})
	})

	return route.Empty{}, err
}
