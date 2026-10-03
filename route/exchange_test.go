package route_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/route"
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
