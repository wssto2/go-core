package route

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

type exchangeKey struct{}

// Exchange is the part of the HTTP exchange a typed handler may need and its
// signature leaves out: where the caller is, what browser, the cookies sent
// and the cookies to set. Read it from the context the handler is called with.
//
//	func (h *Handler) login(ctx context.Context, in LoginInput) (Session, error) {
//	    x := route.ExchangeOf(ctx)
//	    ... x.ClientIP(), x.UserAgent()
//	    x.SetCookie(&http.Cookie{Name: "session", Value: token, HttpOnly: true})
//	}
//
// Outside a route, the zero Exchange answers empty and sets nothing, so a
// service that uses it can also be called from a test.
type Exchange struct{ c *gin.Context }

// ExchangeOf returns the exchange of the request ctx belongs to.
func ExchangeOf(ctx context.Context) Exchange {
	x, _ := ctx.Value(exchangeKey{}).(Exchange)
	return x
}

// ClientIP is the caller's address, as gin resolves it with the trusted proxies.
func (x Exchange) ClientIP() string {
	if x.c == nil {
		return ""
	}

	return x.c.ClientIP()
}

// Path is the path of the request URL, as the client sent it.
func (x Exchange) Path() string {
	if x.c == nil {
		return ""
	}

	return x.c.Request.URL.Path
}

// UserAgent is the User-Agent header.
func (x Exchange) UserAgent() string { return x.Header("User-Agent") }

// Header is a request header.
func (x Exchange) Header(name string) string {
	if x.c == nil {
		return ""
	}

	return x.c.GetHeader(name)
}

// Secure reports whether the request came over HTTPS, directly or through a
// proxy that says so with X-Forwarded-Proto.
func (x Exchange) Secure() bool {
	if x.c == nil {
		return false
	}

	return x.c.Request.TLS != nil || x.c.GetHeader("X-Forwarded-Proto") == "https"
}

// Cookie is the value of a request cookie, empty when there is none.
func (x Exchange) Cookie(name string) string {
	if x.c == nil {
		return ""
	}

	value, err := x.c.Cookie(name)
	if err != nil {
		return ""
	}

	return value
}

// SetCookie adds a Set-Cookie header to the response.
func (x Exchange) SetCookie(cookie *http.Cookie) {
	if x.c == nil {
		return
	}

	http.SetCookie(x.c.Writer, cookie)
}
