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
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/route"
)

type createInput struct {
	ID    int    `path:"id"`
	Page  int    `query:"page"`
	Title string `json:"title" validation:"required|max:10"`
}

func newEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(middlewares.ErrorHandler(slog.New(slog.DiscardHandler), nil, false))

	return e
}

func do(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
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
	if err := r.Mount(e, nil); err != nil {
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
	_ = r.Mount(e, nil)

	if rec := do(e, http.MethodGet, "/x", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestPermissionIsChecked(t *testing.T) {
	r := route.Get[route.None, string]("/x").Requires("a.b:view").To(func(context.Context, route.None) (string, error) {
		return "ok", nil
	})

	denied := newEngine()
	if err := r.Mount(denied, authztest.DenyAll()); err != nil {
		t.Fatal(err)
	}

	if rec := do(denied, http.MethodGet, "/x", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}

	allowed := newEngine()
	_ = r.Mount(allowed, authztest.AllowAll())

	if rec := do(allowed, http.MethodGet, "/x", ""); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestMountWithoutAuthorizerSaysHowToFix(t *testing.T) {
	r := route.Get[route.None, string]("/x").Requires("a.b:view").To(func(context.Context, route.None) (string, error) {
		return "", nil
	})

	err := r.Mount(newEngine(), nil)
	if err == nil || !strings.Contains(err.Error(), "gocore.WithAuthorizer") {
		t.Fatalf("want an error naming the fix, got %v", err)
	}
}

func TestNonStructInputIsRejected(t *testing.T) {
	r := route.Get[int, string]("/x").To(func(context.Context, int) (string, error) { return "", nil })
	if err := r.Mount(newEngine(), nil); err == nil || !strings.Contains(err.Error(), "route.None") {
		t.Fatalf("want error pointing at route.None, got %v", err)
	}
}

func TestUnhandledCoversRawRoutes(t *testing.T) {
	typed := route.Get[route.None, string]("/a")
	raw := route.Raw(http.MethodGet, "/stream")
	route.Group(typed, raw)

	missing := route.Unhandled(typed.To(func(context.Context, route.None) (string, error) { return "", nil }))
	if len(missing) != 1 || missing[0].Path != "/stream" {
		t.Fatalf("want the raw route reported, got %v", missing)
	}

	bound := raw.To(func(*gin.Context) {})
	if got := route.Unhandled(bound); len(got) != 0 && got[0].Path == "/stream" {
		t.Fatalf("a bound raw route is not missing: %v", got)
	}
}
