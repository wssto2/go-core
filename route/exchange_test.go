package route_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/web"
)

type Greeting struct {
	Seen string `json:"seen"`
}

var Greet = route.Post[route.None, Greeting]("/greet").Public()

func greet(ctx context.Context, _ route.None) (Greeting, error) {
	x := route.ExchangeOf(ctx)
	x.SetCookie(&http.Cookie{Name: "seen", Value: "yes", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})

	return Greeting{Seen: x.Cookie("visit") + "|" + x.UserAgent() + "|" + x.ClientIP()}, nil
}

func TestExchangeReadsTheRequestAndSetsCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	require.NoError(t, Greet.To(greet).Mount(engine, route.Security{}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/greet", nil)
	req.Header.Set("User-Agent", "test-agent")
	req.AddCookie(&http.Cookie{Name: "visit", Value: "7"}) //nolint:gosec // a request cookie in a test
	req.RemoteAddr = "192.0.2.1:1234"

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":{"seen":"7|test-agent|192.0.2.1"}}`, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Set-Cookie"), "seen=yes")
	assert.Contains(t, rec.Header().Get("Set-Cookie"), "HttpOnly")
}

func TestExchangeOutsideARouteIsEmpty(t *testing.T) {
	x := route.ExchangeOf(context.Background())
	x.SetCookie(&http.Cookie{Name: "a", Value: "b", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})

	assert.Empty(t, x.ClientIP())
	assert.Empty(t, x.UserAgent())
	assert.Empty(t, x.Cookie("a"))
}

// ExchangeOf gives a handler the client's address, User-Agent and cookies, and
// lets it set cookies, which its typed signature cannot.
func ExampleExchangeOf() {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	_ = Greet.To(func(ctx context.Context, _ route.None) (Greeting, error) {
		route.ExchangeOf(ctx).SetCookie(&http.Cookie{Name: "seen", Value: "yes", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		return Greeting{Seen: route.ExchangeOf(ctx).UserAgent()}, nil
	}).Mount(engine, route.Security{})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/greet", nil)
	req.Header.Set("User-Agent", "curl")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	fmt.Println(rec.Body.String(), rec.Header().Get("Set-Cookie"))
	// Output: {"success":true,"data":{"seen":"curl"}} seen=yes; HttpOnly; Secure; SameSite=Lax
}

//nolint:cyclop,noinlineerr,varnamelen,paralleltest // one scenario table in the style of the package's tests
func TestSetHeaderOnSuccessAndError(t *testing.T) {
	e := newEngine()
	mustMount := func(r route.Bound) {
		if err := r.Mount(e, secured(authztest.AllowAll())); err != nil {
			t.Fatal(err)
		}
	}

	mustMount(route.Get[route.None, string]("/ok").To(func(ctx context.Context, _ route.None) (string, error) {
		return "v", route.ExchangeOf(ctx).SetHeader("Cache-Control", "private, max-age=300")
	}))
	mustMount(route.Get[route.None, string]("/bad").To(func(ctx context.Context, _ route.None) (string, error) {
		_ = route.ExchangeOf(ctx).SetHeader("Cache-Control", "no-store")

		return "", apperr.BadRequest("nope")
	}))
	mustMount(route.Get[route.None, string]("/ct").To(func(ctx context.Context, _ route.None) (string, error) {
		return "", route.ExchangeOf(ctx).SetHeader("content-type", "text/plain")
	}))

	w := do(e, http.MethodGet, "/ok", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, max-age=300" {
		t.Fatalf("ok: %d %v", w.Code, w.Header())
	}

	ref := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(ref)
	web.JSON(c, 200, "v")

	if w.Body.String() != ref.Body.String() || w.Header().Get("Content-Type") != ref.Header().Get("Content-Type") {
		t.Fatalf("envelope differs: %q vs %q", w.Body.String(), ref.Body.String())
	}

	if w = do(e, http.MethodGet, "/bad", ""); w.Code != 400 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("error: %d %v", w.Code, w.Header())
	}

	if w = do(e, http.MethodGet, "/ct", ""); w.Code != 500 {
		t.Fatalf("reserved: %d", w.Code)
	}

	for _, name := range []string{"Content-Type", "content-length"} {
		if err := (route.Exchange{}).SetHeader(name, "x"); !errors.Is(err, route.ErrReservedHeader) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
