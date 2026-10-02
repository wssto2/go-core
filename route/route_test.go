package route_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/route"
)

type createInput struct {
	ID    int    `path:"id"`
	Page  int    `query:"page"`
	Title string `json:"title" validation:"required|max:10"`
}

func secured(a authz.Authorizer) route.Security { return signedIn(a) }

func open() route.Security { return signedIn(nil) }

func newEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(middlewares.ErrorHandler(slog.New(slog.DiscardHandler), nil, false))

	return e
}

func do(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestBindsPathQueryAndBodyThenValidates(t *testing.T) {
	var got createInput
	r := route.Post[createInput, route.Empty]("/t/:id").To(func(_ context.Context, in createInput) (route.Empty, error) {
		got = in
		return route.Empty{}, nil
	})

	e := newEngine()
	if err := r.Mount(e, open()); err != nil {
		t.Fatal(err)
	}

	if rec := do(e, http.MethodPost, "/t/5?page=2", `{"title":"hi"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	if got.ID != 5 || got.Page != 2 || got.Title != "hi" {
		t.Fatalf("bound %+v", got)
	}

	if rec := do(e, http.MethodPost, "/t/5", `{"title":"way too long a title"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want validation failure, got %d: %s", rec.Code, rec.Body)
	}

	if rec := do(e, http.MethodPost, "/t/abc", `{"title":"hi"}`); rec.Code == http.StatusNoContent {
		t.Fatalf("a non-numeric path id must be rejected")
	}
}

func TestHandlerErrorMapsThroughApperr(t *testing.T) {
	r := route.Get[route.None, string]("/x").To(func(context.Context, route.None) (string, error) {
		return "", apperr.NotFound("ticket")
	})

	e := newEngine()
	_ = r.Mount(e, open())

	if rec := do(e, http.MethodGet, "/x", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestPermissionIsChecked(t *testing.T) {
	r := route.Get[route.None, string]("/x").Requires("a.b:view").To(func(context.Context, route.None) (string, error) {
		return "ok", nil
	})

	denied := newEngine()
	if err := r.Mount(denied, secured(authztest.DenyAll())); err != nil {
		t.Fatal(err)
	}

	if rec := do(denied, http.MethodGet, "/x", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}

	allowed := newEngine()
	_ = r.Mount(allowed, secured(authztest.AllowAll()))

	if rec := do(allowed, http.MethodGet, "/x", ""); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestMountWithoutAuthorizerSaysHowToFix(t *testing.T) {
	r := route.Get[route.None, string]("/x").Requires("a.b:view").To(func(context.Context, route.None) (string, error) {
		return "", nil
	})

	err := r.Mount(newEngine(), secured(nil))
	if err == nil || !strings.Contains(err.Error(), "gocore.WithAuthorizer") {
		t.Fatalf("want an error naming the fix, got %v", err)
	}
}

func TestNonStructInputIsRejected(t *testing.T) {
	r := route.Get[int, string]("/x").To(func(context.Context, int) (string, error) { return "", nil })
	if err := r.Mount(newEngine(), secured(nil)); err == nil || !strings.Contains(err.Error(), "route.None") {
		t.Fatalf("want error pointing at route.None, got %v", err)
	}
}

func TestUnhandledCoversRawRoutes(t *testing.T) {
	typed := route.Get[route.None, string]("/a")
	raw := route.Raw(http.MethodGet, "/stream")
	route.Group("t", typed, raw)

	missing := route.Unhandled(typed.To(func(context.Context, route.None) (string, error) { return "", nil }))
	if len(missing) != 1 || missing[0].Path != "/stream" {
		t.Fatalf("want the raw route reported, got %v", missing)
	}

	bound := raw.To(func(*gin.Context) {})
	if got := route.Unhandled(bound); len(got) != 0 && got[0].Path == "/stream" {
		t.Fatalf("a bound raw route is not missing: %v", got)
	}
}

func rejectAll(c *gin.Context) {
	_ = c.Error(apperr.Unauthorized("user not authenticated"))
	c.Abort()
}

func TestNonPublicRoutesAreBehindAuthenticationAndPublicOnesAreNot(t *testing.T) {
	handler := func(context.Context, route.None) (string, error) { return "ok", nil }
	security := route.Security{Authenticate: []gin.HandlerFunc{rejectAll}}

	e := newEngine()
	if err := route.Get[route.None, string]("/private").To(handler).Mount(e, security); err != nil {
		t.Fatal(err)
	}

	if err := route.Get[route.None, string]("/open").Public().To(handler).Mount(e, security); err != nil {
		t.Fatal(err)
	}

	if rec := do(e, http.MethodGet, "/private", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("private: status %d", rec.Code)
	}

	if rec := do(e, http.MethodGet, "/open", ""); rec.Code != http.StatusOK {
		t.Fatalf("open: status %d", rec.Code)
	}
}

func TestMountWithoutAuthenticationSaysHowToFix(t *testing.T) {
	handler := func(context.Context, route.None) (string, error) { return "", nil }

	err := route.Get[route.None, string]("/x").To(handler).Mount(newEngine(), route.Security{})
	if err == nil || !strings.Contains(err.Error(), "gocore.WithAuthentication") || !strings.Contains(err.Error(), ".Public()") {
		t.Fatalf("want an error naming both fixes, got %v", err)
	}

	err = route.Get[route.None, string]("/y").Public().Requires("a.b:view").To(handler).Mount(newEngine(), signedIn(nil))
	if err == nil || !strings.Contains(err.Error(), "Public") {
		t.Fatalf("a public route cannot require a permission, got %v", err)
	}
}
