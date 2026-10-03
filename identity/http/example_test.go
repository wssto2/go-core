package http_test

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/identity/account"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/route"
)

// serve mounts the identity routes over memory stores holding one account, ana
// (password "secret"), and returns the engine.
func serve(cfg identityhttp.Config, prefix ...string) (*gin.Engine, identitytest.Kit) {
	kit := identitytest.New(exampleT{}, []account.Account{identitytest.Account(1, "ana", "secret")})
	cfg.Services, cfg.Clock = account.Services{SignIn: kit.SignIn, Users: kit.Users}, kit.Clock

	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(middlewares.ErrorHandler(slog.New(slog.DiscardHandler), nil, true))

	security := route.Security{Authenticate: []gin.HandlerFunc{identityhttp.Authentication(kit.SignIn, cfg.Cookies, cfg.Principal)}}

	var routes gin.IRoutes = engine
	if len(prefix) > 0 {
		routes = engine.Group(prefix[0]) // what gocore.WithPrefix does
	}

	for _, r := range identityhttp.NewHandler(cfg).Routes() {
		_ = r.Mount(routes, security)
	}

	return engine, kit
}

// NewHandler serves the sign-in routes. Behind an application prefix, the refresh
// cookie goes to the refresh route where it is served.
func ExampleNewHandler() {
	engine, _ := serve(identityhttp.Config{}, "/api")

	req := httptest.NewRequestWithContext(context.Background(), nethttp.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"login":"ana","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	fmt.Println(rec.Code, rec.Result().Cookies()[0].Name, rec.Result().Cookies()[1].Path)
	// Output: 200 access_token /api/v1/auth/refresh
}

// Authentication guards every route that is not public, and leaves the account
// and session for the handlers.
func ExampleAuthentication() {
	engine, kit := serve(identityhttp.Config{})

	signed, _ := kit.SignIn.Login(context.Background(), account.LoginInput{Login: "ana", Password: "secret"})

	for _, token := range []string{"", signed.Credentials.Access} {
		req := httptest.NewRequestWithContext(context.Background(), nethttp.MethodGet, "/v1/auth/me", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		fmt.Println(rec.Code)
	}
	// Output:
	// 401
	// 200
}

// AuthenticatedFrom reads who the middleware found, inside a handler.
func ExampleAuthenticatedFrom() {
	engine, kit := serve(identityhttp.Config{})
	signed, _ := kit.SignIn.Login(context.Background(), account.LoginInput{Login: "ana", Password: "secret"})

	whoami := route.Get[route.None, string]("/whoami")
	_ = whoami.To(func(ctx context.Context, _ route.None) (string, error) {
		who, _ := identityhttp.AuthenticatedFrom(ctx)
		return who.Account.Login, nil
	}).Mount(engine, route.Security{Authenticate: []gin.HandlerFunc{identityhttp.Authentication(kit.SignIn, identityhttp.Cookies{}, nil)}})

	req := httptest.NewRequestWithContext(context.Background(), nethttp.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+signed.Credentials.Access)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	fmt.Println(rec.Body.String())
	// Output: {"success":true,"data":"ana"}
}

// DefaultUser is the payload's user unless the application projects its own:
// the public fields, never the password hash.
func ExampleDefaultUser() {
	user, _ := identityhttp.DefaultUser(context.Background(), account.Account{ID: 1, Login: "ana", Name: "Ana", PasswordHash: "secret"})
	fmt.Printf("%+v\n", user)
	// Output: {ID:1 Login:ana Name:Ana Email: Locale:}
}

// DefaultPrincipal acts as the person, with no location of their own.
func ExampleDefaultPrincipal() {
	p, _ := identityhttp.DefaultPrincipal(context.Background(), account.Account{ID: 7})
	fmt.Println(p.Subject, p.Location)
	// Output: user:7 0
}

// A route is a value: its path is relative to the prefix, and it is public only
// where it must be.
func ExampleLogin() {
	for _, r := range []route.Spec{identityhttp.Login.Spec(), identityhttp.Me.Spec(), identityhttp.LoginAs.Spec()} {
		fmt.Println(r.Method, r.Path, "public:", r.Public)
	}
	// Output:
	// POST /v1/auth/login public: true
	// GET /v1/auth/me public: false
	// POST /v1/auth/login-as public: false
}

// Cookies renames the cookies and places them.
func ExampleCookies() {
	engine, _ := serve(identityhttp.Config{Cookies: identityhttp.Cookies{Access: "sid", Refresh: "rid"}})

	req := httptest.NewRequestWithContext(context.Background(), nethttp.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"login":"ana","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	fmt.Println(rec.Result().Cookies()[0].Name, rec.Result().Cookies()[1].Name)
	// Output: sid rid
}

// exampleT lets an Example use the helpers that take a testing.TB.
type exampleT struct{ testing.TB }

func (exampleT) Helper()                      {}
func (exampleT) Context() context.Context     { return context.Background() }
func (exampleT) Fatalf(f string, args ...any) { log.Fatalf(f, args...) }
