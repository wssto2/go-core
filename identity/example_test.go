package identity_test

import (
	"context"
	"fmt"
	"log"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/gormstore"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/identity/mailtext"
	"github.com/wssto2/go-core/mail"
	"github.com/wssto2/go-core/navigation"
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

// newApp is a test application with an authorizer that allows everything and the
// users permissions defined, which the users routes need to start.
func newApp(t testing.TB, opts ...gocoretest.Option) *gocore.App {
	t.Helper()

	cat := authz.NewCatalogue()
	if err := identity.DefinePermissions(cat); err != nil {
		t.Fatal(err)
	}

	app := gocoretest.New(t, append([]gocoretest.Option{gocoretest.Authorizer(authztest.AllowAll())}, opts...)...)
	app.Permissions(cat)

	return app
}

func signIn(app interface {
	Handler() (nethttp.Handler, error)
}) string {
	handler, err := app.Handler()
	if err != nil {
		return err.Error()
	}

	req := httptest.NewRequestWithContext(context.Background(), nethttp.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"login":"ana","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return fmt.Sprint(rec.Code)
}

// Install is one line: the routes, the tables and the authentication.
func ExampleInstall() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	users := identity.Install(app, identity.WithoutMail())

	_, _ = gormstore.New(app.Database()).Accounts.Create(context.Background(), identitytest.Account(1, "ana", "secret"))

	acc, err := users.Get(context.Background(), 1)
	fmt.Println(acc.Login, err, signIn(app))
	// Output: ana <nil> 200
}

func ExampleOn() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t, gocoretest.Databases("local", "shared"))
	identity.Install(app, identity.WithoutMail(), identity.On("shared"))

	fmt.Println(app.Database("shared").Migrator().HasTable("accounts"), app.Database().Migrator().HasTable("accounts"))
	// Output: true false
}

func ExampleWithAccounts() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	legacy := identitytest.NewAccounts() // really: a store over the application's own users table
	identity.Install(app, identity.WithoutMail(), identity.WithAccounts(legacy), identity.WithHasher(identitytest.Hasher))

	_, _ = legacy.Create(context.Background(), identitytest.Account(1, "ana", "secret"))

	fmt.Println(signIn(app))
	// Output: 200
}

func ExampleWithAccess() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithAccess(fakeAccess{})) // really: the *authz.Engine
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleWithNavigation() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithNavigation(navigation.Node{I18n: "nav.home", Route: "home"}))
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleAllowImpersonation() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.AllowImpersonation("identity.account:impersonate"))
	fmt.Println(app.Check())
	// Output: <nil>
}

// Routes is the contract: read it without an application.
func ExampleRoutes() {
	for _, spec := range identity.Routes.Specs() {
		if strings.HasPrefix(spec.Path, "/v1/auth/") || spec.Path == "/v1/iam/profile" || spec.Path == "/v1/iam/users" {
			fmt.Println(strings.TrimSpace(spec.Method + " " + spec.Path + " " + spec.Permission))
		}
	}

	fmt.Println(len(identity.Routes.Specs()), "routes in all")
	// Output:
	// POST /v1/auth/login
	// POST /v1/auth/refresh
	// POST /v1/auth/logout
	// GET /v1/auth/me
	// POST /v1/auth/change-locale
	// POST /v1/auth/login-as
	// POST /v1/auth/login-as/return
	// GET /v1/iam/users iam.user:view
	// POST /v1/iam/users iam.user:manage
	// GET /v1/iam/profile
	// PUT /v1/iam/profile
	// 31 routes in all
}

func ExampleWithConfig() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithConfig(identity.Config{Lock: identity.Lock{After: 3}}))
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleWithNotices() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithNotices(account.NoNotices))
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleWithCookies() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithCookies(identity.Cookies{Access: "sid", Refresh: "rid"}))
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleWithRefreshHasher() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithRefreshHasher(auth.NewHMACHasher([]byte("secret"))))
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleWithMail() {
	t := &exampleT{}
	defer t.done()

	// A Sink records the mails, for a test or a first run; mail.SMTP(...) is the real one.
	sink := mail.NewSink()

	app := newApp(t)
	users := identity.Install(app, identity.WithMail(sink), identity.WithCodeSecret("an-installation-secret-of-32-chars!"))

	ctx := context.Background()

	// The first administrator is made in code; the address change then mails its code.
	ana, _ := users.Admin().Create(ctx, account.CreateAccount{
		Login: "ana", Name: "Ana", Email: "ana@old.example", Locale: "en", Password: "a long password",
	})

	_, err := users.Profile().RequestEmailChange(ctx, account.RequestEmail{AccountID: ana.ID, Email: "ana@new.example", CurrentPassword: "a long password"})

	sent, _ := sink.Last()
	fmt.Println(err, sent.To, sent.Subject)
	// Output: <nil> [ana@new.example] Your confirmation code
}

func ExampleWithoutMail() {
	t := &exampleT{}
	defer t.done()

	// No sender: the rest works, the address cannot change by code.
	app := newApp(t)
	users := identity.Install(app, identity.WithoutMail())

	ana, _ := users.Admin().Create(context.Background(), account.CreateAccount{
		Login: "ana", Name: "Ana", Email: "ana@old.example", Locale: "en", Password: "a long password",
	})

	_, err := users.Profile().RequestEmailChange(context.Background(), account.RequestEmail{AccountID: ana.ID, Email: "ana@new.example", CurrentPassword: "a long password"})
	fmt.Println(apperr.HasReason(err, account.ReasonEmailDisabled))
	// Output: true
}

func ExampleWithMailContent() {
	t := &exampleT{}
	defer t.done()

	// Write only the mail you want to change; identity's English covers the rest.
	croatian := mail.RendererFunc(func(_ context.Context, _ string, name mail.Name, data any) (mail.Content, error) {
		if name != mailtext.EmailCode {
			return mail.Content{}, mail.ErrNoTemplate
		}

		return mail.Content{Subject: "Vaša šifra", Text: "Šifra: " + data.(mailtext.CodeData).Code}, nil
	})

	app := newApp(t)
	identity.Install(app, identity.WithMail(mail.NewSink()), identity.WithCodeSecret("an-installation-secret-of-32-chars!"), identity.WithMailContent(croatian))
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleWithPasswordPolicy() {
	t := &exampleT{}
	defer t.done()

	// The default is 8 characters; this application wants 12.
	app := newApp(t)
	users := identity.Install(app, identity.WithoutMail(), identity.WithPasswordPolicy(account.Passwords{MinLength: 12}))

	_, err := users.Admin().Create(context.Background(), account.CreateAccount{
		Login: "ana", Name: "Ana", Email: "ana@example.com", Locale: "en", Password: "only ten ch",
	})
	fmt.Println(apperr.HasReason(err, account.ReasonPasswordWeak))
	// Output: true
}

func ExampleWithDeactivationHook() {
	t := &exampleT{}
	defer t.done()

	// The application refuses to deactivate somebody who still owns open leads.
	ownsLeads := identity.DeactivationHookFunc(func(context.Context, account.Account, int) error {
		return apperr.BadRequest("owns open leads").WithReason("crm.owns_leads")
	})

	app := newApp(t)
	users := identity.Install(app, identity.WithoutMail(), identity.WithDeactivationHook(ownsLeads))

	boris, _ := users.Admin().Create(context.Background(), account.CreateAccount{
		Login: "boris", Name: "Boris", Email: "boris@example.com", Locale: "en", Password: "a long password",
	})

	err := users.Admin().Deactivate(context.Background(), account.DeactivateInput{ID: boris.ID, ActorID: 99})
	fmt.Println(apperr.HasReason(err, "crm.owns_leads"))
	// Output: true
}

func ExampleDefinePermissions() {
	catalogue := authz.NewCatalogue()
	fmt.Println(identity.DefinePermissions(catalogue))

	_, view := catalogue.Lookup(identity.ViewUsers)
	_, manage := catalogue.Lookup(identity.ManageUsers)
	fmt.Println(view, manage)
	// Output:
	// <nil>
	// true true
}

func ExampleWithActivityAreas() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithActivityAreas(
		identity.Area("crm").Types("customers", "offers").Prefix("contracts."),
		identity.Area("vehicles").Types("vehicles"),
	))
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleArea() {
	t := &exampleT{}
	defer t.done()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithActivityAreas(identity.Area("other").Types("x")))
	fmt.Println(strings.Contains(fmt.Sprint(app.Check()), `activity area "other" is reserved`))
	// Output: true
}
