package route_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

const trustedNet = "10.0.0.0/8"

// secureOf answers x.Secure() for a request from peer carrying X-Forwarded-Proto: https, and the X-Forwarded-For seen.
//
//nolint:exhaustruct,exhaustruct_v5,lll // a public route needs no security
func secureOf(t *testing.T, proxies []string, peer string, forwardedFor string) (bool, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	require.NoError(t, engine.SetTrustedProxies(proxies))

	var seenXFF string

	require.NoError(t, route.Get[route.None, bool]("/s").Public().To(func(ctx context.Context, _ route.None) (bool, error) {
		secure := route.ExchangeOf(ctx).Secure()
		seenXFF = route.ExchangeOf(ctx).Header("X-Forwarded-For")

		return secure, nil
	}).Mount(engine, route.Security{}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/s", nil)
	req.RemoteAddr = peer + ":1234"
	req.Header.Set("X-Forwarded-Proto", "https")

	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	return strings.Contains(rec.Body.String(), `"data":true`), seenXFF
}

//nolint:paralleltest // gin.SetMode is global
func TestSecureTrustsForwardedProtoOnlyFromATrustedProxy(t *testing.T) {
	secure, _ := secureOf(t, []string{trustedNet}, "10.1.2.3", "")
	assert.True(t, secure, "a trusted proxy says https")

	secure, _ = secureOf(t, []string{trustedNet}, "203.0.113.9", "")
	assert.False(t, secure, "any other client can send the header")

	secure, _ = secureOf(t, nil, "10.1.2.3", "")
	assert.False(t, secure, "no trusted proxies: nobody is trusted")

	secure, _ = secureOf(t, nil, "192.0.2.1", "")
	assert.False(t, secure, "a peer that is the probe address itself")
}

//nolint:paralleltest // gin.SetMode is global
func TestSecureLeavesTheForwardedForHeaderAlone(t *testing.T) {
	_, seen := secureOf(t, []string{trustedNet}, "10.1.2.3", "198.51.100.7, 10.0.0.1")
	assert.Equal(t, "198.51.100.7, 10.0.0.1", seen)

	_, seen = secureOf(t, []string{trustedNet}, "10.1.2.3", "")
	assert.Empty(t, seen)
}
