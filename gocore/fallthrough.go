package gocore

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/proxy"
)

// WithFallthrough hands every request no route answers to h, such as a proxy.New handler in front of the legacy
// site the application replaces one route at a time. A request that matches a route never reaches it.
//
// What runs for such a request: request id, tracing, logging, metrics, panic recovery, the error handler, the body
// limit, the global and per-IP rate limits, and the middleware added with App.Use. Not the security headers (CSP,
// nosniff, HSTS, frame options), WithTrustedOrigins, WithCSP, CORS and idempotency: the other site sets its own
// headers, and nothing reads or buffers its request body.
//
// With WithSPA the shell must be limited to a base path (WithSPAConfig, SPAConfig.BasePath): the shell answers its
// base path and the API prefix keeps its JSON 404, everything else goes to h. A request whose method no route
// accepts is also unmatched, so it goes to h as well.
func WithFallthrough(handler http.Handler) Option {
	return func(app *App) { app.unmatched = handler }
}

// WithFallthrough is WithFallthrough for the builder.
func (b *appBuilder) WithFallthrough(handler http.Handler) *appBuilder {
	b.unmatched = handler

	return b
}

// proxied says the request is one the fallthrough answers: no route matched and the SPA does not own the path.
func (b *appBuilder) proxied(ctx *gin.Context) bool {
	return b.unmatched != nil && ctx.FullPath() == "" && (b.spaConfig == nil || !b.spaConfig.Owns(ctx.Request.URL.Path))
}

// unlessProxied runs next for every request except the proxied ones.
func (b *appBuilder) unlessProxied(next gin.HandlerFunc) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if b.proxied(ctx) {
			ctx.Next()

			return
		}

		next(ctx)
	}
}

// errFallthroughSPA is the wiring error of a shell that would swallow what the fallthrough should answer.
var errFallthroughSPA = errors.New("fallthrough with the SPA needs SPAConfig.BasePath, or the shell answers all")

// fallthroughHandler is the NoRoute handler: it tells the proxy whether the peer is a trusted proxy (the same list gin
// uses for the client IP) and where to report an upstream failure.
func (b *appBuilder) fallthroughHandler() (gin.HandlerFunc, error) {
	trusted := make([]netip.Prefix, 0, len(b.cfg.HTTP.TrustedProxies))

	for _, entry := range b.cfg.HTTP.TrustedProxies {
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			addr, addrErr := netip.ParseAddr(entry)
			if addrErr != nil {
				return nil, fmt.Errorf("trusted proxy %q: %w", entry, err)
			}

			prefix = netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen())
		}

		trusted = append(trusted, prefix)
	}

	return func(ctx *gin.Context) {
		peer, _ := netip.ParseAddr(ctx.RemoteIP())
		isTrusted := slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(peer.Unmap()) })

		// The error is what the incident log and the error handler see; the response is already written.
		report := func(err error) { _ = ctx.Error(err) }
		peerInfo := proxy.Peer{Trusted: isTrusted, Failed: report}
		ctx.Request = ctx.Request.WithContext(proxy.WithPeer(ctx.Request.Context(), peerInfo))
		b.unmatched.ServeHTTP(ctx.Writer, ctx.Request)
	}, nil
}
