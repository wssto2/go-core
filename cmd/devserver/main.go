// Command devserver serves go-core's identity, access and notification modules for
// developing a front end against them. Development only.
//
//	go run github.com/wssto2/go-core/cmd/devserver
//
// It serves the API under /api on 127.0.0.1:8090 over an in-memory SQLite
// database that starts empty on every run and is seeded with an administrator
// and a plain user (their logins and passwords are printed at start), who has two
// things in her activity, the second done while the administrator was signed in as
// her, and two notifications in her inbox (one read, one unread) of the sample category
// "devserver.sample", which is e-mailed by default. The event queue is drained and the due e-mails are sent every
// second here, so the test notification (POST /api/v1/notifications/test) arrives. No mail leaves this machine:
// identity's codes and the notification e-mails go to an in-memory sink and are printed to the log (an e-mail made in the
// user's quiet hours, 21:00 to 07:00 local, is held until they end, as it would be). Links in the e-mails point at the first
// -origin. Browsers at
// http://localhost:5173 (the vue-core playground) may call it with cookies.
//
//	-addr          listen address, default 127.0.0.1:8090
//	-origin        allowed browser origin, default http://localhost:5173 (repeatable by commas)
//	-allow-remote  allow a listen address other than this machine, and APP_ENV/GO_ENV=production
//
// It refuses to start in production or on a non-loopback address.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/gormstore"
	"github.com/wssto2/go-core/mail"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/notification"
	"gorm.io/gorm"
)

// person is a seeded account: the login and password are fixed so the README
// and the start-up banner can name them.
type person struct {
	ID       int
	Login    string
	Password string
	Email    string
	Role     string
}

var people = []person{
	{ID: 1, Login: "admin", Password: "admin-password", Email: "admin@dev.test", Role: "webmaster"},
	{ID: 2, Login: "user", Password: "user-password", Email: "user@dev.test", Role: "seller"},
}

type options struct {
	addr        string
	origins     []string
	allowRemote bool
}

func main() {
	var (
		o       options
		origins string
	)

	flag.StringVar(&o.addr, "addr", "127.0.0.1:8090", "listen address")
	flag.StringVar(&origins, "origin", "http://localhost:5173", "browser origins allowed to call the API, separated by commas")
	flag.BoolVar(&o.allowRemote, "allow-remote", false, "allow a non-loopback listen address and a production environment")
	flag.Parse()

	o.origins = strings.Split(origins, ",")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, o, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "devserver:", err)
		os.Exit(1)
	}
}

// refuse says why the development server must not start with these settings.
func refuse(o options, env func(string) string) error {
	if o.allowRemote {
		return nil
	}

	for _, key := range []string{"GO_ENV", "APP_ENV"} {
		if strings.EqualFold(env(key), "production") {
			return fmt.Errorf("%s=production: this server has fixed passwords and an empty in-memory database; run your application, or pass -allow-remote if you really mean it", key)
		}
	}

	host, _, err := net.SplitHostPort(o.addr)
	if err != nil {
		return fmt.Errorf("listen address %q: %w (use host:port, for example 127.0.0.1:8090)", o.addr, err)
	}

	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("listen address %q is reachable from other machines: this server has fixed passwords, use 127.0.0.1:8090, or pass -allow-remote", o.addr)
	}

	return nil
}

func run(ctx context.Context, o options, out io.Writer) error {
	if err := refuse(o, os.Getenv); err != nil {
		return err
	}

	handler, err := build(ctx, o.origins, slog.New(slog.NewTextHandler(out, nil)), time.Now)
	if err != nil {
		return err
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", o.addr)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "go-core devserver on http://%s/api (in-memory database, e-mail codes are printed here)\n", listener.Addr())

	for _, p := range people {
		_, _ = fmt.Fprintf(out, "  sign in as %-6s password %-15s (%s, role %s)\n", p.Login, p.Password, p.Email, p.Role)
	}

	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()

		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second) // ctx is done: the shutdown needs a live one
		defer cancel()

		_ = srv.Shutdown(shutdown)
	}()

	if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

// clock is a gocore.Clock made of a function: the application's time is the one the server was built with.
type clock func() time.Time

func (c clock) Now() time.Time { return c() }

// build installs identity and access over a fresh in-memory database, seeds the
// people and answers with CORS for the origins.
func build(ctx context.Context, origins []string, log *slog.Logger, now func() time.Time) (http.Handler, error) {
	reg, _ := database.NewTestRegistry("local") // in-memory SQLite, gone with the process

	app := gocore.New(bootstrap.DefaultConfig(), gocore.WithRegistry(reg), gocore.WithLogger(log),
		gocore.WithPrefix("/api"), gocore.WithAutoMigrate(ctx), gocore.WithClock(clock(now)))

	permissions := authz.NewCatalogue()
	permissions.MustDefine("crm.customer:view")
	permissions.MustDefine("iam.user:impersonate", authz.Sensitive()) // the administrator may sign in as user: the playground's banner

	if err := notification.DefinePermissions(permissions); err != nil { // the dead-letter routes; the webmaster holds them
		return nil, err
	}

	sink := mail.NewSink()
	printed := mail.SenderFunc(func(ctx context.Context, m mail.Message) error {
		if err := sink.Send(ctx, m); err != nil {
			return err
		}

		log.Info("mail", "to", strings.Join(m.To, ", "), "subject", m.Subject, "text", m.Text)

		return nil
	})

	//nolint:contextcheck // installing builds the routes; no request exists yet
	users := identity.Install(app, identity.WithMail(printed), identity.WithCodeSecret("devserver-secret-not-for-production!"),
		identity.AllowImpersonation("iam.user:impersonate"),
		identity.WithActivityAreas(identity.Area("crm").Types("customers")))
	//nolint:contextcheck // installing builds the routes; no request exists yet
	acc := access.Install(app, permissions, users, access.WithRoles(
		authz.Role{Key: "seller", Name: "Seller", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")},
		authz.ComputedRole("webmaster", "Webmaster", authz.All()),
	))

	//nolint:contextcheck // installing builds the routes; no request exists yet
	notices := notification.Install(app, users, sampleCategory, notification.AppURL(strings.TrimSpace(origins[0])))

	gin.DebugPrintRouteFunc = func(string, string, string, int) {} // the route table is in the README, not in the log

	inner, err := app.Handler() //nolint:contextcheck // the handler makes its own request contexts
	if err != nil {
		return nil, err
	}

	store := gormstore.New(app.Database())

	for _, p := range people {
		hash, err := account.Bcrypt{}.Hash(p.Password)
		if err != nil {
			return nil, err
		}

		_, err = store.Accounts.Create(ctx, account.Account{
			ID: p.ID, Login: p.Login, Name: p.Login, Email: p.Email, Locale: "en", Active: true, PasswordHash: hash, CreatedAt: now(),
		})
		if err != nil {
			return nil, fmt.Errorf("seed %s: %w", p.Login, err)
		}

		if err := acc.Seed(ctx, authz.Subject{Kind: authz.KindUser, ID: p.ID}, p.Role); err != nil {
			return nil, fmt.Errorf("seed %s: %w", p.Login, err)
		}
	}

	if err := seedActivity(ctx, app.Database(), now()); err != nil {
		return nil, fmt.Errorf("seed activity: %w", err)
	}

	if err := seedNotices(ctx, app, notices); err != nil {
		return nil, fmt.Errorf("seed notifications: %w", err)
	}

	go drainEvents(ctx, app, notices, log)

	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(requestLog(log), middlewares.Cors(middlewares.CorsConfig{
		AllowOrigins: origins,
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", "Accept-Language", "X-Request-ID", "Idempotency-Key"},
	}))
	engine.NoRoute(gin.WrapH(inner))

	return engine, nil
}

// seedActivity gives the user a failed sign-in, a change made to her and two things she did, an hour ago and a minute later, so the
// Activity section has something to show: the second was done while the administrator was signed
// in as her (a session opened for the purpose and last used then).
func seedActivity(ctx context.Context, db *gorm.DB, now time.Time) error {
	at := now.UTC().Add(-time.Hour).Truncate(time.Second)

	session, err := gormstore.New(db).Sessions.Open(ctx, account.NewSession{
		AccountID: 2, ActorID: 1, Device: "dev seed", At: at.Add(30 * time.Second), ExpiresAt: at.Add(24 * time.Hour),
	})
	if err != nil {
		return err
	}

	if err := db.WithContext(ctx).Model(&auth.Token{}).Where("token_value = ?", session.Access).
		Update("last_used_at", at.Add(2*time.Minute)).Error; err != nil {
		return err
	}

	// A refused attempt on her history, and a change the administrator made to her: the Sign-ins
	// (Failed) and Changes sections have something to show, the change naming who made it.
	if err := gormstore.New(db).SignIns.Record(ctx, account.SignInEntry{
		AccountID: 2, Event: account.WrongPassword, IP: "203.0.113.7", Device: "dev seed", CreatedAt: at.Add(45 * time.Minute),
	}); err != nil {
		return err
	}

	if err := gormstore.NewChangeLog(db).Record(ctx, account.Change{
		AccountID: 2, ActorID: 1, Action: account.ChangeUpdated, Fields: []string{"phone"}, Before: map[string]string{"phone": ""}, After: map[string]string{"phone": "+385 1 555 0100"},
	}); err != nil {
		return err
	}

	for _, row := range []struct {
		action string
		at     time.Time
	}{{"create", at}, {"update", at.Add(time.Minute)}} {
		err := db.WithContext(ctx).Exec(
			"INSERT INTO audit_logs (entity_type, entity_id, action, actor_id, created_at) VALUES ('customers', 41, ?, 2, ?)", row.action, row.at,
		).Error
		if err != nil {
			return err
		}
	}

	return nil
}

// sampleCategory is the one category the playground's inbox has.
var sampleCategory = notification.Category("devserver.sample").EmailByDefault()

// sampled is the event the seed sends notifications through, the way a feature's event would.
var sampled = event.Define[sample]("devserver.sample")

type sample struct {
	UserID int    `json:"user_id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Link   string `json:"link"`
}

// seedNotices puts two notifications in user's inbox, through the event queue as a feature would: the
// older one read, the newer one unread, so the bell shows one.
func seedNotices(ctx context.Context, app *gocore.App, notices *notification.Notices) error {
	app.Events(sampled.To("devserver.sample", func(ctx context.Context, s sample) error {
		return notices.Send(ctx, sampleCategory, notification.To(s.UserID), func(notification.Recipient) notification.Message {
			return notification.Message{Title: s.Title, Body: s.Body, Link: s.Link, Data: map[string]string{"sample": "true"}}
		})
	}))

	tx := database.NewTransactor(app.Database())

	for _, s := range []sample{
		{UserID: 2, Title: "Your account is ready", Body: "Welcome to the playground."},
		{UserID: 2, Title: "A ticket was assigned to you", Body: "Fix the sign-in page", Link: "/tickets/7"},
	} {
		err := tx.WithinTransaction(ctx, func(ctx context.Context) error { return sampled.Publish(ctx, s) })
		if err != nil {
			return err
		}
	}

	if err := app.DrainEvents(ctx); err != nil {
		return err
	}

	page, err := notices.Inbox.List(ctx, 2, notification.ListQuery{})
	if err != nil {
		return err
	}

	_, err = notices.Inbox.MarkRead(ctx, 2, page.Items[len(page.Items)-1].ID) // the older one

	return err
}

// drainEvents stands in for the background workers Run starts: this server serves the handler only, so
// the event queue (the test notification) is handled and the due e-mails are sent here, every second, until ctx ends.
func drainEvents(ctx context.Context, app *gocore.App, notices *notification.Notices, log *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := app.DrainEvents(ctx); err != nil && ctx.Err() == nil {
				log.Warn("events", "error", err)
			}

			if _, err := notices.DeliverDue(ctx); err != nil && ctx.Err() == nil {
				log.Warn("e-mail", "error", err)
			}
		}
	}
}

func requestLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		log.Info("request", "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status())
	}
}
