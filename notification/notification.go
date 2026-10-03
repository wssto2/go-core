// Package notification is the in-app inbox of an application: the notifications
// a person finds when they open it, read on one device and read on all of them.
//
// A feature never writes a notification. It publishes an event for what
// happened, and a consumer of that event sends the notifications, in the
// recipients' own language:
//
//	var TicketAssigned = notification.Category("tickets.assigned") // declared once, as a value
//
//	notices := notification.Install(app, users, TicketAssigned)
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
// The module has no translations of its own: the application renders the text
// for each recipient, in the language Recipient.Locale names. Categories are
// declared in code and registered with Install; the inbox routes, the live
// stream and the dead-letter routes come with it.
package notification

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/notification/migrations"
	"gorm.io/gorm"
)

// Category is the kind of a notification: a code such as "tickets.assigned",
// lower-case words joined by dots or dashes, at most CategoryMax characters.
// Declare the application's categories once, as values, and register them with
// Install (NOTIF-CATEGORY-001):
//
//	var TicketAssigned = notification.Category("tickets.assigned")
type Category string

// CategoryMax is the longest category code, the width of its column.
const CategoryMax = 64

// systemTest is the module's own category, for the test notification
// (POST /v1/notifications/test). It is reserved: an application cannot register it.
const systemTest Category = "system.test"

var categoryPattern = regexp.MustCompile(`^[a-z][a-z0-9]*([.-][a-z0-9]+)*$`)

// String is the category's code.
func (c Category) String() string { return string(c) }

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
// invalid ids, duplicates and the excepted are dropped, the order kept.
func (r Recipients) resolve() []int {
	out := make([]int, 0, len(r.ids))

	for _, id := range r.ids {
		if id <= 0 || slices.Contains(out, id) || slices.Contains(r.except, id) {
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

	people     People
	categories []Category
	db         *gorm.DB
	store      *store
	clock      gocore.Clock
}

// Install puts the in-app inbox into app: the notifications table (its
// migration, which the application runs with "./app migrate"), the inbox
// routes and the live stream. categories are the application's own; the module's
// test category is added by it. users names the people to notify.
//
//	notices := notification.Install(app, users, TicketAssigned, TicketCommented)
//
// A bad or repeated category code is a start-up problem Run lists with the fix.
func Install(app *gocore.App, users People, categories ...Category) *Notices {
	if users == nil {
		app.Fail("notification needs the people to notify", "pass the users as the second argument: notification.Install(app, users, categories...)")

		return &Notices{}
	}

	if problems := categoryProblems(categories); len(problems) > 0 {
		for _, p := range problems {
			app.Fail(p.What, p.Fix)
		}

		return &Notices{}
	}

	db := app.Database()
	st := &store{db: db}

	app.Schema(gocore.Schema{Files: migrations.Files, Models: Migrate})

	n := &Notices{
		people: users, categories: append(slices.Clone(categories), systemTest), db: db, store: st, clock: app.Clock(),
	}
	n.Inbox = &Inbox{store: st, clock: app.Clock()}

	return n
}

type problem struct{ What, Fix string }

// categoryProblems says what is wrong with the categories given to Install.
func categoryProblems(categories []Category) []problem {
	var out []problem

	seen := map[Category]bool{}

	for _, c := range categories {
		switch {
		case !categoryPattern.MatchString(string(c)) || len(c) > CategoryMax:
			out = append(out, problem{fmt.Sprintf("notification category %q is not lower-case words joined by dots or dashes, at most %d characters", c, CategoryMax),
				"declare it like notification.Category(\"tickets.assigned\")"})
		case c == systemTest:
			out = append(out, problem{fmt.Sprintf("notification category %q is the module's own", c), "choose another code for your category"})
		case seen[c]:
			out = append(out, problem{fmt.Sprintf("notification category %q is registered twice", c), "declare each category once and pass it to Install once"})
		}

		seen[c] = true
	}

	return out
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
func (n *Notices) Send(ctx context.Context, category Category, to Recipients, render func(Recipient) Message) error {
	if n.store == nil {
		return errors.New("notification: Send on a Notices that Install did not build: see the problems Run reports")
	}

	eventID, ok := event.ID(ctx)
	if !ok {
		return fmt.Errorf("%w: publish an event and send from its consumer, "+
			"app.Events(MyEvent.To(\"notifications.my-event\", func(ctx, e) error { return notices.Send(ctx, ...) }))", ErrNotInConsumer)
	}

	if _, inTx := database.TxFromContext(ctx); inTx {
		return fmt.Errorf("%w: Send commits its own transaction, so it cannot run inside another", ErrNotInConsumer)
	}

	if !slices.Contains(n.categories, category) {
		return fmt.Errorf("%w: %w: %q: pass it to notification.Install", event.ErrMalformed, ErrUnknownCategory, category)
	}

	if render == nil {
		return fmt.Errorf("%w: %w: render is nil", event.ErrMalformed, ErrInvalidMessage)
	}

	ids := to.resolve()
	if len(ids) == 0 {
		return nil
	}

	accounts, err := n.people.Find(ctx, ids)
	if err != nil {
		return fmt.Errorf("notification: look up the recipients: %w", err)
	}

	byID := make(map[int]account.Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
	}

	now := n.clock.Now()
	rows := make([]row, 0, len(ids))

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
			return fmt.Errorf("%w: %w (recipient %d, category %q)", event.ErrMalformed, err, id, category)
		}

		r, err := newRow(eventID, id, category, msg, now)
		if err != nil {
			return fmt.Errorf("%w: %w (recipient %d, category %q)", event.ErrMalformed, err, id, category)
		}

		rows = append(rows, r)
	}

	if len(rows) == 0 {
		return nil
	}

	_, err = n.store.insertNew(ctx, rows)

	return err
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
func DedupeKey(eventID uint64, userID int, category Category) string {
	return fmt.Sprintf("%d:%d:%s", eventID, userID, category)
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
