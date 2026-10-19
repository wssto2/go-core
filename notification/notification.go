// Package notification is the in-app inbox of an application: the notifications
// a person finds when they open it, read on one device and read on all of them.
//
// A feature never writes a notification. It publishes an event for what
// happened, and a consumer of that event sends the notifications, in the
// recipients' own language:
//
//	var TicketAssigned = notification.Category("tickets.assigned") // declared once, as a value
//
//	notices := notification.Install(app, users, TicketAssigned, notification.AppURL("https://tickets.example.com"))
//
//	app.Events(tickets.Assigned.To("notifications.ticket-assigned",
//		func(ctx context.Context, e tickets.Assigned) error {
//			return notices.Send(ctx, TicketAssigned, notification.To(e.AssigneeID).Except(e.ActorID),
//				func(r notification.Recipient) notification.Message { // once per recipient
//					return notification.Message{
//						Title: t(r.Locale, "tickets.assigned", e.Title),
//						Link:  fmt.Sprintf("/tickets/%d", e.TicketID),
//					}
//				})
//		}))
//
// Send works only inside an event consumer (NOTIF-EVENT-001): the id of the
// event it handles makes the notification's dedupe key, so an event delivered
// twice, or retried after a failure, makes no second notification. A rolled-back
// write never notifies and a committed one always does, because the event is
// queued in the write's transaction.
//
// The module has no translations of its own for the notification: the application renders the text
// for each recipient, in the language Recipient.Locale names. Categories are
// declared in code and registered with Install; the inbox routes, the live
// stream, the settings routes and the dead-letter routes come with it.
//
// When identity has mail, a person is also e-mailed the categories whose e-mail is on for them: what
// Enforce says, else their own choice, else the category's default (NOTIF-PREF-001), held while their
// quiet hours last (NOTIF-QUIET-001) and sent once by a worker that retries and gives up
// (NOTIF-DELIVERY-001). With identity.WithoutMail() e-mail is unavailable and the settings say so.
// The text of the e-mail is in mailtext, in English, Croatian, Bosnian and Slovenian.
//
// With WithPush the same goes for web push: a person registers their browsers as devices (Notices.Devices), and a
// notification of a category whose push is on for them is pushed to each of them, held in quiet hours, retried,
// and bundled in a burst (NOTIF-DEVICE-001). The protocol is package notification/push, standard library only.
package notification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/notification/migrations"
	"github.com/wssto2/go-core/notification/push"
	"gorm.io/gorm"
)

// The widths of the stored text. A rendered title or body longer than its
// column is cut to fit and ends in an ellipsis, rather than failing the event
// (NOTIF-CONTENT-001).
const (
	TitleMax = 160
	BodyMax  = 500
	LinkMax  = 255
)

// Message is what one recipient sees: rendered by the application, in the
// recipient's language (NOTIF-CONTENT-001).
type Message struct {
	// Title is required, cut to TitleMax characters.
	Title string
	// Body is optional, cut to BodyMax characters. Never a customer's phone
	// number or e-mail address.
	Body string
	// Link is the in-app path the notification opens, such as "/tickets/7"; empty
	// opens nothing. An absolute URL, "//host", a backslash or a line break is
	// refused, so a notification can never send a person off-site.
	Link string
	// Data is optional ids and facts for the client, flat. Never secrets.
	Data map[string]string
}

// Recipient is a person about to be notified, as the render function of Send
// sees them.
type Recipient struct {
	ID int
	// Name is the person's name, or their login when they have none.
	Name string
	// Locale is the person's language, a BCP-47 tag such as "hr" or "en".
	Locale string
}

// Recipients is who a notification is for: To lists them, Except takes some away.
type Recipients struct {
	ids    []int
	except []int
}

// To names the people a notification is for, by account id. Ids that are not
// valid, duplicates and people who are not active are dropped (NOTIF-RECIPIENT-001).
func To(ids ...int) Recipients { return Recipients{ids: ids} }

// Except takes people out of the recipients, usually the person who did what is
// being announced: nobody is told about their own action.
//
//	notification.To(e.AssigneeID).Except(e.ActorID)
func (r Recipients) Except(ids ...int) Recipients {
	r.except = append(slices.Clone(r.except), ids...)

	return r
}

// resolve applies the recipient rules that need no user data (NOTIF-RECIPIENT-001):
// invalid ids (not between 1 and MaxInt32), duplicates and the excepted are dropped, the order kept.
func (r Recipients) resolve() []int {
	out := make([]int, 0, len(r.ids))

	for _, id := range r.ids {
		if id <= 0 || id > math.MaxInt32 || slices.Contains(out, id) || slices.Contains(r.except, id) {
			continue
		}

		out = append(out, id)
	}

	return out
}

// Errors of Send. A message the application's render function made unusable is
// wrapped in event.ErrMalformed too, so the queue sets the event aside as a
// dead letter at once instead of retrying what cannot change.
var (
	// ErrNotInConsumer: Send was called outside an event consumer.
	ErrNotInConsumer = errors.New("notification: Send must be called from an event consumer")
	// ErrUnknownCategory: the category was not registered with Install.
	ErrUnknownCategory = errors.New("notification: category is not registered")
	// ErrInvalidMessage: the rendered message has no title, or a link that is not an in-app path.
	ErrInvalidMessage = errors.New("notification: invalid message")
)

// errNotInstalled is the answer of a Notices that Install did not build.
var errNotInstalled = errors.New(
	"notification: Send on a Notices that Install did not build: see the problems Run reports",
)

// People is what the module asks of the application's users: the accounts of
// many ids, and a person's live sessions for the stream to re-check. *identity.Users
// is one.
type People interface {
	// Find returns the accounts that exist among ids, active or not, in one query.
	Find(ctx context.Context, ids []int) ([]account.Account, error)
	// Sessions lists the account's live sessions.
	Sessions(ctx context.Context, accountID int) ([]account.Session, error)
}

// Notices is what Install built: Send makes notifications from an event
// consumer, Inbox is what a person reads them with. After Install reported a
// problem to the application (Run lists it) the value is empty.
type Notices struct {
	// Inbox lists, counts and reads a person's notifications.
	Inbox *Inbox
	// Settings is what a person chooses for their own notifications: e-mail and push per category, and quiet hours.
	Settings *Settings
	// Devices are a person's push devices: register this browser, list, rename, remove and test them.
	Devices *Devices

	people     People
	kinds      []Kind
	location   *time.Location
	mail       *account.Mail
	push       *push.Sender
	bundleText func(locale string, count int, titles []string) (title, body string)
	// testText words the test notification; nil is the default English text.
	testText  func(Recipient) Message
	appURL    string
	db        *gorm.DB
	store     *store
	metrics   *metrics
	clock     gocore.Clock
	log       *slog.Logger
	heartbeat time.Duration
}

// Install puts the in-app inbox into app: the notifications table (its
// migration, which the application runs with "./app migrate"), the inbox
// routes, the live stream and, for every consumer of the application, the dead-letter routes (they
// need DefinePermissions on the permission catalogue). users names the people to notify. After them
// come the application's categories (see Category) and the options AppURL, TimeZone and Enforce, in one
// list; the module's test category is added by Install.
//
//	notices := notification.Install(app, users, TicketAssigned, TicketCommented)
//
// A bad or repeated category code or option is a start-up problem Run lists with the fix.
func Install(app *gocore.App, users People, opts ...Option) *Notices {
	defer app.Shipping()()

	if users == nil {
		app.Fail("notification needs the people to notify", "pass the users as the second argument: notification.Install(app, users, categories...)")

		return &Notices{}
	}

	cfg := collect(opts)

	if len(cfg.problems) > 0 {
		for _, p := range cfg.problems {
			app.Fail(p.What, p.Fix)
		}

		return &Notices{}
	}

	db := app.Database()
	meters := newMetrics(app.Telemetry())
	notes := &store{db: db, metrics: meters}

	app.Schema(gocore.Schema{Files: migrations.Files, Models: Migrate})

	n := &Notices{
		people: users, kinds: append(slices.Clone(cfg.kinds), systemTest), location: cfg.location, appURL: cfg.appURL,
		db: db, store: notes, metrics: meters, clock: app.Clock(), log: app.Logger(), heartbeat: heartbeatInterval,
	}
	n.mail = mailOf(users)

	if n.mail != nil && cfg.appURL == "" {
		app.Fail("e-mail links need notification.AppURL(...): identity sends mail, so notifications are e-mailed, and an e-mail links to the application",
			"pass notification.AppURL(\"https://your-app.example.com\") to notification.Install")
	}

	n.Inbox = &Inbox{store: notes, clock: app.Clock(), hub: NewHub()}
	n.push, n.bundleText, n.testText = cfg.push, cfg.bundleText, cfg.testText

	if n.bundleText == nil {
		n.bundleText = defaultBundleText
	}

	n.Settings = &Settings{
		store: notes, kinds: n.kinds, enforce: cfg.enforce, emailOK: n.mail != nil, pushOK: n.push != nil,
		clock: app.Clock(), location: cfg.location,
	}
	n.Devices = &Devices{store: notes, sender: n.push, clock: app.Clock(), db: db}

	app.Events(n.testConsumer())
	app.Background(housekeeper{notices: n})

	if n.mail != nil {
		app.Background(&deliveryWorker{notices: n})
	}

	if n.push != nil {
		app.Events(n.deviceTestConsumer())
		app.Background(&pushWorker{notices: n})
	}

	app.Routes(Declare().To(n)...)
	app.Routes(DeclareDeadLetters().To(event.NewDeadLetters(db, app.Clock()), app.Consumers)...)

	return n
}

// mailOf is where identity's mail goes, when the people are identity's Users and it has mail: e-mail is
// available only then. People that have no Mail method, or identity run WithoutMail, mean no e-mail.
func mailOf(users People) *account.Mail {
	m, ok := users.(interface{ Mail() (account.Mail, bool) })
	if !ok {
		return nil
	}

	mail, ok := m.Mail()
	if !ok || mail.Sender == nil || mail.Renderer == nil {
		return nil
	}

	return &mail
}

// kind is the registered category with the code of k: its defaults are the registered ones.
func (n *Notices) kind(k Kind) (Kind, bool) {
	i := slices.IndexFunc(n.kinds, func(r Kind) bool { return r.code == k.code })
	if i < 0 {
		return Kind{}, false
	}

	return n.kinds[i], true
}

// Send makes a notification of the category for each of the recipients and
// puts it in their inbox. render is called once per recipient, with who they
// are and the language they use, and returns what they should read.
//
//	return notices.Send(ctx, TicketAssigned, notification.To(e.AssigneeID).Except(e.ActorID),
//		func(r notification.Recipient) notification.Message {
//			return notification.Message{Title: t(r.Locale, "tickets.assigned", e.Title), Link: "/tickets/7"}
//		})
//
// Send is for the handler of an event consumer and fails anywhere else. The
// notification is keyed on the event's id, the person and the category, so a
// second delivery of the same event, a retry after a failure included, makes no
// second notification and tells nobody twice (NOTIF-EVENT-001). Everyone's
// notification is written in one transaction: either all of them or, on an
// error, none, and the event is retried. A message that can never be valid, or a
// category that was not registered, is a dead letter at once (event.ErrMalformed).
//
// People who are not accounts or not active, invalid and repeated ids, and the
// ones Except named are skipped (NOTIF-RECIPIENT-001). Once the transaction
// has committed, the open apps of the recipients are told (NOTIF-READ-001).
func (n *Notices) Send(ctx context.Context, category Kind, to Recipients, render func(Recipient) Message) error {
	return n.send(ctx, category, to, render, 0)
}

// send is Send, with the pushes limited to one device when onlyDevice is not 0 (the test of a device).
func (n *Notices) send(
	ctx context.Context,
	category Kind,
	recipients Recipients,
	render func(Recipient) Message,
	onlyDevice uint64,
) error {
	eventID, registered, err := n.checkSend(ctx, category, render)
	if err != nil {
		return err
	}

	ids := recipients.resolve()
	if len(ids) == 0 {
		return nil
	}

	now := n.clock.Now()

	rows, notified, err := n.render(ctx, eventID, category, ids, render, now)
	if err != nil || len(rows) == 0 {
		return err
	}

	plans, err := n.plans(ctx, registered, notified, whole(now), onlyDevice)
	if err != nil {
		return err
	}

	created, made, err := n.store.insertNew(ctx, rows, plans)
	if err != nil {
		return err
	}

	n.noteCreated(created, made)
	n.announce(ctx, created)

	return nil
}

// checkSend refuses a Send that cannot work: not from an event consumer, inside a transaction, an unregistered category
// or no render function. It returns the event's id and the registered category.
func (n *Notices) checkSend(ctx context.Context, category Kind, render func(Recipient) Message) (uint64, Kind, error) {
	if n.store == nil {
		return 0, Kind{}, errNotInstalled
	}

	eventID, ok := event.ID(ctx)
	if !ok {
		return 0, Kind{}, fmt.Errorf("%w: publish an event and send from its consumer, "+
			"app.Events(MyEvent.To(\"notifications.my-event\", func(ctx, e) error { return notices.Send(ctx, ...) }))", ErrNotInConsumer)
	}

	if _, inTx := database.TxFromContext(ctx); inTx {
		return 0, Kind{}, fmt.Errorf(
			"%w: Send commits its own transaction, so it cannot run inside another",
			ErrNotInConsumer,
		)
	}

	registered, ok := n.kind(category)
	if !ok {
		return 0, Kind{}, fmt.Errorf(
			"%w: %w: %q: pass it to notification.Install",
			event.ErrMalformed,
			ErrUnknownCategory,
			category.code,
		)
	}

	if render == nil {
		return 0, Kind{}, fmt.Errorf("%w: %w: render is nil", event.ErrMalformed, ErrInvalidMessage)
	}

	return eventID, registered, nil
}

// render makes the notification of each recipient who is an active account, and returns the rows with those accounts.
func (n *Notices) render(
	ctx context.Context, eventID uint64, category Kind, ids []int, render func(Recipient) Message, now time.Time,
) ([]row, []account.Account, error) {
	accounts, err := n.people.Find(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("notification: look up the recipients: %w", err)
	}

	byID := make(map[int]account.Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
	}

	rows := make([]row, 0, len(ids))
	notified := make([]account.Account, 0, len(ids))

	for _, id := range ids {
		a, found := byID[id]
		if !found || !a.Active {
			continue
		}

		name := a.Name
		if name == "" {
			name = a.Login
		}

		msg, err := finish(render(Recipient{ID: id, Name: name, Locale: a.Locale}))
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w (recipient %d, category %q)", event.ErrMalformed, err, id, category.code)
		}

		r, err := newRow(eventID, id, category, msg, now)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w (recipient %d, category %q)", event.ErrMalformed, err, id, category.code)
		}

		rows = append(rows, r)
		notified = append(notified, a)
	}

	return rows, notified, nil
}

// finish applies the content rules to a rendered message: the title is
// required, the title and body are cut to their columns, the link must be an
// in-app path (NOTIF-CONTENT-001).
func finish(m Message) (Message, error) {
	m.Title = truncate(strings.TrimSpace(m.Title), TitleMax)
	if m.Title == "" {
		return Message{}, fmt.Errorf("%w: the title is empty", ErrInvalidMessage)
	}

	m.Link = strings.TrimSpace(m.Link)
	if !IsAppLink(m.Link) {
		return Message{}, fmt.Errorf("%w: the link %q is not an in-app path such as \"/tickets/7\"", ErrInvalidMessage, m.Link)
	}

	m.Body = truncate(strings.TrimSpace(m.Body), BodyMax)

	return m, nil
}

// IsAppLink reports whether link is empty or a path inside the app. A
// notification never links off-site: "//host" and "https://..." are refused, so a
// tapped push cannot become an open redirect (NOTIF-CONTENT-001).
func IsAppLink(link string) bool {
	if link == "" {
		return true
	}

	if len(link) > LinkMax || !strings.HasPrefix(link, "/") || strings.HasPrefix(link, "//") {
		return false
	}

	return !strings.ContainsAny(link, "\\\r\n")
}

// DedupeKey is the idempotency key of a notification: one per event, person and
// category, so an event delivered twice creates no second notification
// (NOTIF-EVENT-001).
func DedupeKey(eventID uint64, userID int, category Kind) string {
	return fmt.Sprintf("%d:%d:%s", eventID, userID, category.code)
}

// truncate cuts value to maxRunes characters, the last of which is an ellipsis
// when something was cut.
func truncate(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}

	runes := []rune(value)

	return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
}
