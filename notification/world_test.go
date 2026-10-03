package notification_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/identity/mailtext"
	"github.com/wssto2/go-core/mail"
	"github.com/wssto2/go-core/notification"
	"gorm.io/gorm"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

// directory is the people of a test: the real in-memory users, counting how often it was asked and
// failing on demand.
type directory struct {
	*account.Users

	mu       sync.Mutex
	finds    int
	failNext int
}

func (d *directory) Find(ctx context.Context, ids []int) ([]account.Account, error) {
	d.mu.Lock()
	d.finds++

	if d.failNext > 0 {
		d.failNext--
		d.mu.Unlock()

		return nil, errors.New("the directory is away")
	}

	d.mu.Unlock()

	return d.Users.Find(ctx, ids)
}

func (d *directory) Finds() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.finds
}

func (d *directory) FailNext(n int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.failNext = n
}

// world is an application over one database with the inbox installed and four people: Ana (1, hr), Ivo (2,
// en), Eva (3, en) and Max (4, inactive).
type world struct {
	t       *testing.T
	db      *gorm.DB
	clock   *fakeClock
	app     *gocore.App
	notices *notification.Notices
	people  *directory
	handler http.Handler
	// sink is where e-mail goes: nil when the world has no mail (identity run WithoutMail).
	sink *mail.Sink
}

// assignedEvent is the fact of the tests, carrying the TicketAssigned category of the examples.
type assignedEvent struct {
	TicketID int   `json:"ticket_id"`
	To       []int `json:"to"`
	Actor    int   `json:"actor"`
}

var assigned = event.Define[assignedEvent]("tickets.assigned")

func accounts() []account.Account {
	ana, ivo, eva, mx := identitytest.Account(1, "ana", "x"), identitytest.Account(2, "ivo", "x"), identitytest.Account(3, "", "x"), identitytest.Account(4, "max", "x")
	ana.Locale, ana.Name, ana.Email = "hr", "Ana Horvat", "ana@example.test"
	ivo.Name, ivo.Email = "Ivo", "ivo@example.test"
	eva.Name, eva.Login, eva.Email = "", "eva", "" // no name: the login shows; no address: no e-mail
	mx.Active = false

	return []account.Account{ana, ivo, eva, mx}
}

// newWorld builds the application on db, with the authorizer given (nil allows everything). Requests are
// made as the person named in the X-Person header.
func newWorld(t *testing.T, db *gorm.DB, authorizer authz.Authorizer) *world {
	t.Helper()

	return buildWorld(t, db, authorizer, nil)
}

// newMailWorld is newWorld with identity's mail going to a sink, and the application's address, so the people
// can be e-mailed. Ana (1) has ana@example.test, Ivo (2) ivo@example.test; Eva (3) has no address.
func newMailWorld(t *testing.T, db *gorm.DB, opts ...notification.Option) *world {
	t.Helper()

	return buildWorld(t, db, nil, mail.NewSink(), append([]notification.Option{notification.AppURL("https://tickets.example.test")}, opts...)...)
}

func buildWorld(t *testing.T, db *gorm.DB, authorizer authz.Authorizer, sink *mail.Sink, opts ...notification.Option) *world {
	t.Helper()

	if authorizer == nil {
		authorizer = authztest.AllowAll()
	}

	w := &world{t: t, db: db, sink: sink, clock: &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}
	w.people = &directory{Users: identitytest.Users(t, accounts()...)}

	if sink != nil {
		w.people.SetMail(account.Mail{Sender: sink, Renderer: mailtext.Defaults})
	}

	reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
	reg.AddConnection("local", db)

	w.app = gocore.New(bootstrap.DefaultConfig(), gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithAutoMigrate(t.Context()), gocore.WithClock(w.clock), gocore.WithAuthorizer(authorizer),
		gocore.WithAuthentication(func(c *gin.Context) {
			id, err := strconv.Atoi(c.GetHeader("X-Person"))
			if err != nil {
				_ = c.Error(apperr.Unauthorized("not signed in"))
				c.Abort()

				return
			}

			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.User(id, 0)))
			c.Next()
		}))

	cat := authz.NewCatalogue()
	require.NoError(t, notification.DefinePermissions(cat))
	w.app.Permissions(cat)

	w.notices = notification.Install(w.app, w.people, append([]notification.Option{TicketAssigned, TicketCommented}, opts...)...)

	return w
}

// start migrates (the real files, on a server database) and builds the handler; call it after the consumers are collected.
func (w *world) start() *world {
	w.t.Helper()

	require.NoError(w.t, w.app.Migrate(w.t.Context()))

	handler, err := w.app.Handler()
	require.NoError(w.t, err)

	w.handler = handler

	return w
}

// consumeAssigned collects the consumer a feature would write: it sends the tickets.assigned category to the
// event's recipients, except the actor, and renders the person's name and language into the title.
func (w *world) consumeAssigned(opts ...event.RetryOption) {
	w.app.Events(assigned.To("notifications.ticket-assigned", func(ctx context.Context, e assignedEvent) error {
		return w.notices.Send(ctx, TicketAssigned, notification.To(e.To...).Except(e.Actor),
			func(r notification.Recipient) notification.Message {
				return notification.Message{Title: "Hi " + r.Name + " (" + r.Locale + ")", Link: "/tickets/" + strconv.Itoa(e.TicketID)}
			})
	}).Retry(opts...))
}

func (w *world) publish(p assignedEvent) {
	w.t.Helper()
	w.publishEvent(assigned, p)
}

func publishOn[T any](t *testing.T, db *gorm.DB, ev event.Event[T], p T) {
	t.Helper()

	require.NoError(t, database.NewTransactor(db).WithinTransaction(t.Context(), func(ctx context.Context) error {
		return ev.Publish(ctx, p)
	}))
}

func (w *world) publishEvent(ev event.Event[assignedEvent], p assignedEvent) {
	w.t.Helper()
	publishOn(w.t, w.db, ev, p)
}

// drain handles every due event and returns what the handlers returned.
func (w *world) drain() error { return w.app.DrainEvents(w.t.Context()) }

func (w *world) inbox(person int) notification.Page {
	w.t.Helper()

	page, err := w.notices.Inbox.List(w.t.Context(), person, notification.ListQuery{Limit: notification.MaxPageSize})
	require.NoError(w.t, err)

	return page
}

func (w *world) titles(person int) []string {
	w.t.Helper()

	out := []string{}
	for _, n := range w.inbox(person).Items {
		out = append(out, n.Title)
	}

	return out
}

func (w *world) count(table string) int64 {
	w.t.Helper()

	var n int64
	require.NoError(w.t, w.db.Table(table).Count(&n).Error)

	return n
}

// do calls the API as a person (0: nobody is signed in).
func (w *world) do(person int, method, path string, body any) *httptest.ResponseRecorder {
	w.t.Helper()

	var reader *bytes.Reader

	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(w.t, err)

		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequestWithContext(w.t.Context(), method, path, reader)
	req.Header.Set("Content-Type", "application/json")

	if person > 0 {
		req.Header.Set("X-Person", strconv.Itoa(person))
	}

	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)

	return rec
}

// data decodes the "data" of a response.
func data[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var envelope struct {
		Data T `json:"data"`
	}

	require.Less(t, rec.Code, 300, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope), rec.Body.String())

	return envelope.Data
}
