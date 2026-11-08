package gocore

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/middlewares"
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

// trustedPeers parses the TRUSTED_PROXIES entries (addresses or CIDRs) once.
func trustedPeers(entries []string) ([]netip.Prefix, error) {
	trusted := make([]netip.Prefix, 0, len(entries))

	for _, entry := range entries {
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

	return trusted, nil
}

// forwardFunc hands a request to the fallthrough handler, see forwardTo.
type forwardFunc func(w http.ResponseWriter, r *http.Request, path string, report func(error))

// forwardTo hands the request to handler and commits its answer. path, when not empty, replaces the request path for
// handler (the query stays); the request gin sees, logs and meters keeps its own. handler is told whether the peer is
// a trusted proxy (the same list gin uses for the client IP) and where to report an upstream failure.
func forwardTo(
	handler http.Handler, trusted []netip.Prefix, writer http.ResponseWriter, req *http.Request, path string,
	report func(error),
) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(req.RemoteAddr))
	if err != nil {
		host = req.RemoteAddr
	}

	peer, _ := netip.ParseAddr(host)
	isTrusted := slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(peer.Unmap()) })

	if path != "" {
		req = req.Clone(req.Context())
		req.URL.Path, req.URL.RawPath = path, ""
	}

	// The error is what the incident log and the error handler see; the response is already written.
	req = req.WithContext(proxy.WithPeer(req.Context(), proxy.Peer{Trusted: isTrusted, Failed: report}))
	handler.ServeHTTP(writer, req)
	// Commit the status h set: gin only counts a Write as an answer, and otherwise replaces a bodyless 404 (a HEAD,
	// an empty upstream 404) with its own text/plain "404 page not found". A handler that wrote nothing at all
	// keeps gin's pre-set 404, now empty.
	if gw, ok := writer.(interface{ WriteHeaderNow() }); ok {
		gw.WriteHeaderNow()
	}
}

// fallthroughHandler is the NoRoute handler.
func (b *appBuilder) fallthroughHandler() (gin.HandlerFunc, error) {
	trusted, err := trustedPeers(b.cfg.HTTP.TrustedProxies)
	if err != nil {
		return nil, err
	}

	b.forward = func(ctx *gin.Context, path string) {
		forwardTo(b.unmatched, trusted, ctx.Writer, ctx.Request, path, func(err error) { _ = ctx.Error(err) })
	}
	b.forwardHTTP = func(writer http.ResponseWriter, req *http.Request, path string, report func(error)) {
		forwardTo(b.unmatched, trusted, writer, req, path, report)
	}

	return func(ctx *gin.Context) { b.forward(ctx, "") }, nil
}

// errNoFallthrough is the wiring error of App.Forward without WithFallthrough.
var errNoFallthrough = errors.New("gocore: Forward needs WithFallthrough")

// Forward answers the request from the fallthrough handler (WithFallthrough) and aborts the chain: call it from a route
// or a middleware to hand a request that would otherwise be the application's to the site behind it. path, when not
// empty, is the path the fallthrough sees instead of the request's (the query stays); the request the rest of go-core
// logs and meters keeps its own.
//
// The fallthrough gets the peer as it does for an unmatched request (trusted or not, and where to report a failure),
// and the answer carries none of go-core's security or CORS headers already set on it, as for an unmatched request.
// Without WithFallthrough the request is an internal error.
func (a *App) Forward(ctx *gin.Context, path string) {
	ctx.Abort()

	if a.forward == nil {
		_ = ctx.Error(apperr.Internal(errNoFallthrough))

		return
	}

	middlewares.StripSecurityHeaders(ctx.Writer.Header())
	a.forward(ctx, path)
}

// ForwardRequest is Forward for a handler or middleware written on net/http: it answers the request from the
// fallthrough handler (WithFallthrough) and the caller must not write or call next after it. path, when not empty,
// is the path the fallthrough sees (the query stays). The fallthrough gets the peer, and the answer loses go-core's
// security and CORS headers, as for Forward.
//
// Without a gin context to hand an upstream failure to, a failure is logged, not an incident. Without WithFallthrough
// the answer is a plain 500 and the cause is logged. Prefer it to Forward; Forward stays for gin handlers.
func (a *App) ForwardRequest(writer http.ResponseWriter, req *http.Request, path string) {
	if a.forwardHTTP == nil {
		a.log.Error("forward_request_failed", "error", errNoFallthrough)
		http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	middlewares.StripSecurityHeaders(writer.Header())
	a.forwardHTTP(writer, req, path, func(err error) { a.log.Error("forward_request_failed", "error", err) })
}
