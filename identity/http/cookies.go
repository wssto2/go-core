package http

import (
	nethttp "net/http"
	"time"

	"github.com/wssto2/go-core/route"
)

// Cookies name and place the cookies the tokens travel in. The zero value is
// the defaults: access_token for the whole site, refresh_token for the refresh
// route only (under the mount prefix).
type Cookies struct {
	Access  string
	Refresh string
	// AccessPath is the path the access cookie is sent to, default "/".
	AccessPath string
	// RefreshPath is the path the refresh cookie is sent to. The default is the
	// refresh route under the mount prefix.
	RefreshPath string
	// Domain is the cookies' domain, empty for the host that set them.
	Domain string
}

func (c Cookies) withDefaults(prefix string) Cookies {
	if c.Access == "" {
		c.Access = "access_token"
	}

	if c.Refresh == "" {
		c.Refresh = "refresh_token"
	}

	if c.AccessPath == "" {
		c.AccessPath = "/"
	}

	if c.RefreshPath == "" {
		c.RefreshPath = prefix + "/auth/refresh"
	}

	return c
}

// set writes both cookies for a session that ends at expires; the refresh
// cookie outlives the access token, by as long again.
func (c Cookies) set(x route.Exchange, access, refresh string, now, expires time.Time) {
	maxAge := int(expires.Sub(now).Seconds())

	c.write(x, c.Access, access, c.AccessPath, maxAge)
	c.write(x, c.Refresh, refresh, c.RefreshPath, maxAge*2)
}

func (c Cookies) clear(x route.Exchange) {
	c.write(x, c.Access, "", c.AccessPath, -1)
	c.write(x, c.Refresh, "", c.RefreshPath, -1)
}

func (c Cookies) write(x route.Exchange, name, value, path string, maxAge int) {
	//nolint:gosec // Secure follows the request: HTTPS in production, plain HTTP on a developer's machine
	x.SetCookie(&nethttp.Cookie{
		Name: name, Value: value, Path: path, Domain: c.Domain, MaxAge: maxAge,
		Secure: x.Secure(), HttpOnly: true, SameSite: nethttp.SameSiteLaxMode,
	})
}
