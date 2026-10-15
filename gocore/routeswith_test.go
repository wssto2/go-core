//nolint:testpackage // uses the package's testApp
package gocore

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/route"
)

const (
	keyPath     = "/key"
	keyHeader   = "X-Key"
	sessionPath = "/session"
	sessionHdr  = "X-Session"
	thingView   = "things.thing:view"
)

// header is a stand-in chain: it lets a request through when the header holds "ok" and answers its own 401 otherwise.
func header(name, realm string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.GetHeader(name) != "ok" {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"realm": realm})

			return
		}

		ctx.Next()
	}
}

func call(handler http.Handler, path string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

func okRoute(path string) route.Route[route.None, string] {
	return route.Get[route.None, string](path).Authenticated()
}

func publicRoute(path string) route.Route[route.None, string] {
	return route.Get[route.None, string](path).Public()
}

func serve(path string) func(context.Context, route.None) (string, error) {
	return func(context.Context, route.None) (string, error) { return path, nil }
}

func twoChains(t *testing.T, authorizer authz.Authorizer) (*App, route.Security) {
	t.Helper()

	opts := []Option{WithLogger(slog.New(slog.DiscardHandler)), WithAuthentication(header(sessionHdr, "session"))}
	if authorizer != nil {
		opts = append(opts, WithAuthorizer(authorizer))
	}

	app := New(bootstrap.DefaultConfig(), opts...)

	return app, route.Security{Authenticate: []gin.HandlerFunc{header(keyHeader, "key")}, Authorizer: nil}
}

func TestRoutesWithMountsBehindTheAppsOwnChain(t *testing.T) {
	t.Parallel()

	app, own := twoChains(t, nil)
	app.Routes(okRoute(sessionPath).To(serve("session")))
	app.RoutesWith(own, okRoute(keyPath).To(serve("key")), publicRoute("/open").To(serve("open")))

	handler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}

	for _, scenario := range []struct {
		path    string
		headers []string
		status  int
		realm   string
	}{
		{keyPath, []string{keyHeader, "ok"}, http.StatusOK, ""},
		{keyPath, nil, http.StatusUnauthorized, `"key"`},
		{keyPath, []string{sessionHdr, "ok"}, http.StatusUnauthorized, `"key"`},
		{sessionPath, []string{keyHeader, "ok"}, http.StatusUnauthorized, `"session"`},
		{sessionPath, []string{sessionHdr, "ok"}, http.StatusOK, ""},
		{"/open", nil, http.StatusOK, ""},
	} {
		rec := call(handler, scenario.path, scenario.headers...)
		if rec.Code != scenario.status || !strings.Contains(rec.Body.String(), scenario.realm) {
			t.Errorf("%s %v: %d %s, want %d %s",
				scenario.path, scenario.headers, rec.Code, rec.Body, scenario.status, scenario.realm)
		}
	}
}

func TestRoutesWithChecksPermissionsByAuthorizer(t *testing.T) {
	t.Parallel()

	cat := authz.NewCatalogue()
	cat.MustDefine(thingView)

	app, own := twoChains(t, authztest.DenyAll())
	app.Permissions(cat)
	app.RoutesWith(own, okRoute("/denied").Requires(thingView).To(serve("denied")))

	own.Authorizer = authztest.AllowAll()
	app.RoutesWith(own, okRoute("/allowed").Requires(thingView).To(serve("allowed")))

	handler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}

	if rec := call(handler, "/denied", keyHeader, "ok"); rec.Code != http.StatusForbidden {
		t.Errorf("the app's authorizer is the default: %d %s", rec.Code, rec.Body)
	}

	if rec := call(handler, "/allowed", keyHeader, "ok"); rec.Code != http.StatusOK {
		t.Errorf("the chain's own authorizer wins: %d %s", rec.Code, rec.Body)
	}
}

func TestRoutesWithStillRefusesWhatIsWrongAtStartUp(t *testing.T) {
	t.Parallel()

	app, own := twoChains(t, authztest.AllowAll())
	app.RoutesWith(own, okRoute("/undefined").Requires(thingView).To(serve("x")))

	err := app.Check()
	if err == nil || !strings.Contains(err.Error(), thingView) {
		t.Errorf("an undefined permission must stop start-up, got %v", err)
	}

	bare, _ := twoChains(t, nil)
	bare.RoutesWith(route.Security{Authenticate: nil, Authorizer: nil}, okRoute("/nochain").To(serve("x")))

	err = bare.Check()
	if err == nil || !strings.Contains(err.Error(), "no authentication") {
		t.Errorf("a chain without authentication must stop a protected route, got %v", err)
	}
}

type chainUser struct{ id int }

func (u chainUser) GetID() int { return u.id }

func TestRoutesWithHandlerSeesTheChainsUserAndTheRequestID(t *testing.T) {
	t.Parallel()

	app, _ := twoChains(t, nil)
	own := route.Security{Authenticate: []gin.HandlerFunc{func(ctx *gin.Context) {
		auth.SetUser(ctx, chainUser{id: 9})
		ctx.Next()
	}}, Authorizer: nil}

	app.RoutesWith(own, okRoute(keyPath).To(func(ctx context.Context, _ route.None) (string, error) {
		user, ok := auth.UserOf[chainUser](ctx)

		return fmt.Sprintf("%v %d %s", ok, user.id, route.ExchangeOf(ctx).RequestID()), nil
	}))

	handler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}

	rec := call(handler, keyPath, "X-Request-ID", "rid-1")
	if !strings.Contains(rec.Body.String(), "true 9 rid-1") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
