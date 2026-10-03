package route

import "github.com/gin-gonic/gin"

// Under mounts routes below a path prefix, which lets one declaration serve
// at "/auth/login" for the contract and at "/api/v1/auth/login" in an
// application. The declared Spec, and so the generated TypeScript, keeps the
// path without the prefix: the client adds its base URL.
//
//	app.Routes(route.Under("/api/v1", Show.To(show), Close.To(close))...)
func Under(prefix string, routes ...Handled) []Handled {
	out := make([]Handled, len(routes))
	for i, r := range routes {
		out[i] = under{Handled: r, prefix: prefix}
	}

	return out
}

type under struct {
	Handled
	prefix string
}

func (u under) Mount(r gin.IRoutes, security Security) error {
	return u.Handled.Mount(prefixed{IRoutes: r, prefix: u.prefix}, security)
}

type prefixed struct {
	gin.IRoutes
	prefix string
}

func (p prefixed) Handle(method, path string, handlers ...gin.HandlerFunc) gin.IRoutes {
	p.IRoutes.Handle(method, p.prefix+path, handlers...)

	return p
}
