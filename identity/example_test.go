package identity_test

import (
	"context"
	"fmt"
	"log"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/gormstore"
	"github.com/wssto2/go-core/identity/identitytest"
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

	app := gocoretest.New(t)
	users := identity.Install(app)

	_, _ = gormstore.New(app.Database()).Accounts.Create(context.Background(), identitytest.Account(1, "ana", "secret"))

	acc, err := users.Get(context.Background(), 1)
	fmt.Println(acc.Login, err, signIn(app))
	// Output: ana <nil> 200
}

func ExampleOn() {
	t := &exampleT{}
	defer t.done()

	app := gocoretest.New(t, gocoretest.Databases("local", "shared"))
	identity.Install(app, identity.On("shared"))

	fmt.Println(app.Database("shared").Migrator().HasTable("accounts"), app.Database().Migrator().HasTable("accounts"))
	// Output: true false
}

func ExampleWithAccounts() {
	t := &exampleT{}
	defer t.done()

	app := gocoretest.New(t)
	legacy := identitytest.NewAccounts() // really: a store over the application's own users table
	identity.Install(app, identity.WithAccounts(legacy), identity.WithHasher(identitytest.Hasher))

	_, _ = legacy.Create(context.Background(), identitytest.Account(1, "ana", "secret"))

	fmt.Println(signIn(app))
	// Output: 200
}

func ExampleWithAccess() {
	t := &exampleT{}
	defer t.done()

	app := gocoretest.New(t)
	identity.Install(app, identity.WithAccess(fakeAccess{})) // really: the *authz.Engine
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleWithNavigation() {
	t := &exampleT{}
	defer t.done()

	app := gocoretest.New(t)
	identity.Install(app, identity.WithNavigation(navigation.Node{I18n: "nav.home", Route: "home"}))
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleAllowImpersonation() {
	t := &exampleT{}
	defer t.done()

	app := gocoretest.New(t)
	identity.Install(app, identity.AllowImpersonation(authztest.AllowAll(), "identity.account:impersonate"))
	fmt.Println(app.Check())
	// Output: <nil>
}

// Routes is the contract: read it without an application.
func ExampleRoutes() {
	for _, spec := range identity.Routes.Specs() {
		fmt.Println(spec.Method, spec.Path)
	}
	// Output:
	// POST /v1/auth/login
	// POST /v1/auth/refresh
	// POST /v1/auth/logout
	// GET /v1/auth/me
	// POST /v1/auth/change-locale
	// POST /v1/auth/login-as
}

func ExampleWithConfig() {
	t := &exampleT{}
	defer t.done()

	app := gocoretest.New(t)
	identity.Install(app, identity.WithConfig(identity.Config{Lock: identity.Lock{After: 3}}))
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleWithNotices() {
	t := &exampleT{}
	defer t.done()

	app := gocoretest.New(t)
	identity.Install(app, identity.WithNotices(account.NoNotices))
	fmt.Println(app.Check())
	// Output: <nil>
}

func ExampleWithCookies() {
	t := &exampleT{}
	defer t.done()

	app := gocoretest.New(t)
	identity.Install(app, identity.WithCookies(identity.Cookies{Access: "sid", Refresh: "rid"}))
	fmt.Println(app.Check())
	// Output: <nil>
}
