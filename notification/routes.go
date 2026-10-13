package notification

import (
	"context"

	"github.com/wssto2/go-core/apperr"
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

// StreamKinds is the fixed set of changes the live stream carries.
var StreamKinds = route.Enum(StreamCreated, StreamRead, StreamUnread).As("StreamKind")

// SourceKinds is the fixed set of sources a setting can come from.
var SourceKinds = route.Enum(SourceEnforced, SourcePerson, SourceDefault).As("Source")

// SetEmailInput is "turn the e-mail of one category on or off". Email is required: leaving it out is refused, not
// read as "off".
type SetEmailInput struct {
	Category string `path:"category" validation:"required|max:64"`
	Email    *bool  `json:"email" validation:"required"`
}

// SetPushInput is "turn the push of one category on or off". Push is required: leaving it out is refused, not read as
// "off".
type SetPushInput struct {
	Category string `path:"category" validation:"required|max:64"`
	Push     *bool  `json:"push"     validation:"required"`
}

// DeviceInput addresses one of the signed-in person's push devices.
type DeviceInput struct {
	ID int `path:"id" validation:"min:1"`
}

// RenameDeviceInput is "rename one of my devices".
type RenameDeviceInput struct {
	ID    int    `path:"id"    validation:"min:1"`
	Label string `json:"label" validation:"required|max:100"`
}

// UnsubscribeInput names the browser's subscription to remove, as sign-out does.
type UnsubscribeInput struct {
	Endpoint string `json:"endpoint" validation:"required|max:1024"`
}

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
	// Preferences is the person's settings: e-mail per category with its source, whether e-mail is available at all, and quiet hours.
	Preferences route.Route[route.None, Preferences]
	// SetEmail turns the e-mail of one category on or off for the person; an enforced setting answers 422.
	SetEmail route.Route[SetEmailInput, CategorySettings]
	// SetQuietHours sets the person's quiet hours, in minutes after midnight.
	SetQuietHours route.Route[QuietHoursInput, QuietHours]
	// SetPush turns the push of one category on or off for the person; an enforced setting answers 422.
	SetPush route.Route[SetPushInput, CategorySettings]

	// PushConfig says whether push is on and the key to subscribe the browser with.
	PushConfig route.Route[route.None, PushConfig]
	// Devices lists the person's push devices.
	Devices route.Route[route.None, DeviceList]
	// Subscribe registers this browser's subscription; the same browser again is one device, refreshed.
	Subscribe route.Route[Subscription, Device]
	// Unsubscribe removes the person's device with the given endpoint, as sign-out does, and answers the list.
	Unsubscribe route.Route[UnsubscribeInput, DeviceList]
	// RenameDevice changes the label of one device.
	RenameDevice route.Route[RenameDeviceInput, Device]
	// RemoveDevice removes one device and answers the list.
	RemoveDevice route.Route[DeviceInput, DeviceList]
	// TestDevice pushes a test notification to one device and answers the list.
	TestDevice route.Route[DeviceInput, DeviceList]

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

		Preferences:   route.Get[route.None, Preferences](inbox + "/preferences").Name("notification.preferences"),
		SetEmail:      route.Put[SetEmailInput, CategorySettings](inbox + "/preferences/:category").Name("notification.preferences.set-email"),
		SetQuietHours: route.Put[QuietHoursInput, QuietHours](inbox + "/quiet-hours").Name("notification.quiet-hours"),
		SetPush: route.Put[SetPushInput, CategorySettings](
			inbox + "/preferences/:category/push",
		).Name("notification.preferences.set-push"),

		PushConfig: route.Get[route.None, PushConfig](inbox + "/push").Name("notification.push"),
		Devices:    route.Get[route.None, DeviceList](inbox + "/devices").Name("notification.devices"),
		Subscribe:  route.Post[Subscription, Device](inbox + "/devices").Name("notification.device.subscribe"),
		Unsubscribe: route.Post[UnsubscribeInput, DeviceList](
			inbox + "/devices/unsubscribe",
		).Name("notification.device.unsubscribe"),
		RenameDevice: route.Patch[RenameDeviceInput, Device](inbox + "/devices/:id").Name("notification.device.rename"),
		RemoveDevice: route.Delete[DeviceInput, DeviceList](inbox + "/devices/:id").Name("notification.device.remove"),
		TestDevice:   route.Post[DeviceInput, DeviceList](inbox + "/devices/:id/test").Name("notification.device.test"),
	}

	d.group = route.Group("notification", d.List, d.Unread, d.MarkRead, d.MarkAllRead, d.Stream, d.Test, d.Preferences,
		d.SetEmail, d.SetQuietHours, d.SetPush, d.PushConfig, d.Devices, d.Subscribe, d.Unsubscribe, d.RenameDevice,
		d.RemoveDevice, d.TestDevice).
		Types(StreamEvent{}, StreamKinds, SourceKinds) // what the stream sends, and what a setting says: no route names them

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
		d.Stream.To(n.stream()),
		d.Test.To(n.sendTest),
		d.Preferences.To(n.preferences),
		d.SetEmail.To(n.setEmail),
		d.SetQuietHours.To(n.setQuietHours),
		d.SetPush.To(n.setPush),
		d.PushConfig.To(n.pushConfig),
		d.Devices.To(n.devices),
		d.Subscribe.To(n.subscribe),
		d.Unsubscribe.To(n.unsubscribe),
		d.RenameDevice.To(n.renameDevice),
		d.RemoveDevice.To(n.removeDevice),
		d.TestDevice.To(n.testDevice),
	}
}

func (n *Notices) preferences(ctx context.Context, _ route.None) (Preferences, error) {
	id, err := person(ctx)
	if err != nil {
		return Preferences{}, err
	}

	return n.Settings.Get(ctx, id)
}

func (n *Notices) setEmail(ctx context.Context, in SetEmailInput) (CategorySettings, error) {
	id, err := person(ctx)
	if err != nil {
		return CategorySettings{}, err
	}

	if in.Email == nil {
		return CategorySettings{}, apperr.BadRequest("email is required")
	}

	return n.Settings.SetEmail(ctx, id, in.Category, *in.Email)
}

func (n *Notices) setPush(ctx context.Context, input SetPushInput) (CategorySettings, error) {
	personID, err := person(ctx)
	if err != nil {
		return CategorySettings{}, err
	}

	if input.Push == nil {
		return CategorySettings{}, apperr.BadRequest("push is required")
	}

	return n.Settings.SetPush(ctx, personID, input.Category, *input.Push)
}

func (n *Notices) setQuietHours(ctx context.Context, in QuietHoursInput) (QuietHours, error) {
	id, err := person(ctx)
	if err != nil {
		return QuietHours{}, err
	}

	return n.Settings.SetQuietHours(ctx, id, QuietHours(in))
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

func (n *Notices) pushConfig(ctx context.Context, _ route.None) (PushConfig, error) {
	_, err := person(ctx)
	if err != nil {
		return PushConfig{}, err
	}

	return n.Devices.Config(), nil
}

func (n *Notices) devices(ctx context.Context, _ route.None) (DeviceList, error) {
	id, err := person(ctx)
	if err != nil {
		return DeviceList{}, err
	}

	return n.Devices.List(ctx, id)
}

func (n *Notices) subscribe(ctx context.Context, sub Subscription) (Device, error) {
	id, err := person(ctx)
	if err != nil {
		return Device{}, err
	}

	return n.Devices.Register(ctx, id, route.ExchangeOf(ctx).UserAgent(), sub)
}

func (n *Notices) unsubscribe(ctx context.Context, in UnsubscribeInput) (DeviceList, error) {
	return n.thenList(ctx, func(id int) error { return n.Devices.Unregister(ctx, id, in.Endpoint) })
}

func (n *Notices) renameDevice(ctx context.Context, input RenameDeviceInput) (Device, error) {
	id, err := person(ctx)
	if err != nil {
		return Device{}, err
	}

	return n.Devices.Rename(ctx, id, input.ID, input.Label)
}

func (n *Notices) removeDevice(ctx context.Context, in DeviceInput) (DeviceList, error) {
	return n.thenList(ctx, func(id int) error { return n.Devices.Remove(ctx, id, in.ID) })
}

func (n *Notices) testDevice(ctx context.Context, in DeviceInput) (DeviceList, error) {
	return n.thenList(ctx, func(id int) error { return n.Devices.SendTest(ctx, id, in.ID) })
}

// thenList runs a change of the person's devices and answers with the fresh list, so the app re-renders from one shape.
func (n *Notices) thenList(ctx context.Context, change func(personID int) error) (DeviceList, error) {
	personID, err := person(ctx)
	if err != nil {
		return DeviceList{}, err
	}

	err = change(personID)
	if err != nil {
		return DeviceList{}, err
	}

	return n.Devices.List(ctx, personID)
}
