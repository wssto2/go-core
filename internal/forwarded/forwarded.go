// Package forwarded decides whether a request's X-Forwarded-Proto header can be believed.
package forwarded

import "github.com/gin-gonic/gin"

// Secure reports whether the request came over HTTPS, directly or through a trusted proxy (the engine's trusted
// proxies, as for ClientIP) that says so with X-Forwarded-Proto. The header of any other peer is ignored.
func Secure(ctx *gin.Context) bool {
	return ctx.Request.TLS != nil || ctx.GetHeader("X-Forwarded-Proto") == "https" && peerTrusted(ctx)
}

// peerTrusted says whether the connection's peer is one of the engine's trusted proxies. gin exposes no such check, but
// ClientIP believes X-Forwarded-For only from a trusted peer, so the header is set to an address that is not the peer's
// (RFC 5737 documentation addresses) for one call and put back.
func peerTrusted(ctx *gin.Context) bool {
	probe := "192.0.2.1"
	if ctx.RemoteIP() == probe {
		probe = "192.0.2.2"
	}

	header := ctx.Request.Header
	saved, had := header["X-Forwarded-For"]

	header.Set("X-Forwarded-For", probe)
	trusted := ctx.ClientIP() == probe

	if had {
		header["X-Forwarded-For"] = saved
	} else {
		header.Del("X-Forwarded-For")
	}

	return trusted
}
