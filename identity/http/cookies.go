package http

import (
	nethttp "net/http"
	"strings"
	"time"

	"github.com/wssto2/go-core/route"
)

// Cookies name and place the cookies the tokens travel in. The zero value is
// the defaults: access_token for the whole site, refresh_token for the refresh
// route only.
type Cookies struct {
	Access  string
	Refresh string
	// AccessPath is the path the access cookie is sent to, default "/".
	AccessPath string
	// RefreshPath is the path the refresh cookie is sent to. The default is the
	// refresh route where the request that sets it was served, so it follows the
	// application's prefix (/api/v1/auth/refresh). Set it when a proxy rewrites
	// the path the browser sees.
	RefreshPath string
	// Domain is the cookies' domain, empty for the host that set them.
	Domain string
}

func (c Cookies) withDefaults() Cookies {
	if c.Access == "" {
		c.Access = "access_token"
	}

	if c.Refresh == "" {
		c.Refresh = "refresh_token"
	}

	if c.AccessPath == "" {
		c.AccessPath = "/"
	}

	return c
}

// refreshPath is where the refresh cookie goes: RefreshPath, or the refresh
// route as served. The request was served at the declared path of the route
// that handles it behind the application's prefix, so the prefix is what is
// left of the path when the declared one is cut off the end.
func (c Cookies) refreshPath(x route.Exchange, declared string) string {
	if c.RefreshPath != "" {
		return c.RefreshPath
	}

	prefix, _ := strings.CutSuffix(x.Path(), declared)

	return prefix + Refresh.Spec().Path
}

// set writes both cookies for a session that ends at expires; the refresh
// cookie outlives the access token, by as long again.
func (c Cookies) set(x route.Exchange, declared, access, refresh string, now, expires time.Time) {
	maxAge := int(expires.Sub(now).Seconds())

	c.write(x, c.Access, access, c.AccessPath, maxAge)
	c.write(x, c.Refresh, refresh, c.refreshPath(x, declared), maxAge*2)
}

func (c Cookies) clear(x route.Exchange, declared string) {
	c.write(x, c.Access, "", c.AccessPath, -1)
	c.write(x, c.Refresh, "", c.refreshPath(x, declared), -1)
}

func (c Cookies) write(x route.Exchange, name, value, path string, maxAge int) {
	//nolint:gosec // Secure follows the request: HTTPS in production, plain HTTP on a developer's machine
	x.SetCookie(&nethttp.Cookie{
		Name: name, Value: value, Path: path, Domain: c.Domain, MaxAge: maxAge,
		Secure: x.Secure(), HttpOnly: true, SameSite: nethttp.SameSiteLaxMode,
	})
}
