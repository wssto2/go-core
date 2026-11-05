package gocore

import (
	"fmt"
	"net"
	"net/http"

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
func WithFallthrough(h http.Handler) Option {
	return func(app *App) { app.unmatched = h }
}

// WithFallthrough is WithFallthrough for the builder.
func (b *appBuilder) WithFallthrough(h http.Handler) *appBuilder {
	b.unmatched = h

	return b
}

// proxied says the request is one the fallthrough answers: no route matched and the SPA does not own the path.
func (b *appBuilder) proxied(ctx *gin.Context) bool {
	return b.unmatched != nil && ctx.FullPath() == "" && (b.spaConfig == nil || !b.spaConfig.Owns(ctx.Request.URL.Path))
}

// unlessProxied runs h for every request except the proxied ones.
func (b *appBuilder) unlessProxied(h gin.HandlerFunc) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if b.proxied(ctx) {
			ctx.Next()

			return
		}

		h(ctx)
	}
}

// fallthroughHandler is the NoRoute handler: it tells the proxy whether the peer is a trusted proxy (the same list gin
// uses for the client IP) and where to report an upstream failure.
func (b *appBuilder) fallthroughHandler() (gin.HandlerFunc, error) {
	var trusted []*net.IPNet

	for _, entry := range b.cfg.HTTP.TrustedProxies {
		if ip := net.ParseIP(entry); ip != nil {
			bits := 8 * len(ip.To16())
			if ip.To4() != nil {
				bits = 32
			}

			trusted = append(trusted, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})

			continue
		}

		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", entry, err)
		}

		trusted = append(trusted, network)
	}

	return func(ctx *gin.Context) {
		peer := net.ParseIP(ctx.RemoteIP())
		isTrusted := false

		for _, network := range trusted {
			if peer != nil && network.Contains(peer) {
				isTrusted = true

				break
			}
		}

		// The error is what the incident log and the error handler see; the response is already written.
		report := func(err error) { _ = ctx.Error(err) }
		ctx.Request = ctx.Request.WithContext(proxy.WithPeer(ctx.Request.Context(), proxy.Peer{Trusted: isTrusted, Failed: report}))
		b.unmatched.ServeHTTP(ctx.Writer, ctx.Request)
	}, nil
}
