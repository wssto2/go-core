package middlewares

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

type SecurityConfig struct {
	ContentSecurityPolicy string // if empty, use a safe default
	TrustProxy            bool   // if true, trust X-Forwarded-Proto header for HSTS
}

// Helper to generate a random string
func generateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// TrustedOriginsMiddleware extends the Content-Security-Policy set by the
// Security middleware to permit the given origins in img-src and frame-ancestors.
// It also removes X-Frame-Options: DENY so those origins can embed this app in
// an iframe — frame-ancestors is the modern, CSP-based replacement for that header.
//
// Each entry may be a full URL (https://legacy.example.com/any/path) or a bare
// origin (https://legacy.example.com); only the scheme and host are used.
// Empty and unparseable entries are silently ignored.
//
// Register this after Security in the middleware chain (e.g. via AppBuilder.WithTrustedOrigins).
func TrustedOriginsMiddleware(rawURLs ...string) gin.HandlerFunc {
	origins := make([]string, 0, len(rawURLs))
	for _, raw := range rawURLs {
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			continue
		}
		origins = append(origins, parsed.Scheme+"://"+parsed.Host)
	}

	if len(origins) == 0 {
		return func(ctx *gin.Context) { ctx.Next() }
	}

	return func(ctx *gin.Context) {
		csp := ctx.Writer.Header().Get("Content-Security-Policy")
		if csp != "" {
			ctx.Header("Content-Security-Policy", patchCSP(csp, origins))
			// frame-ancestors in CSP supersedes X-Frame-Options in all modern browsers.
			// Deleting DENY allows the trusted origins declared above to iframe this app.
			ctx.Header("X-Frame-Options", "")
		}
		ctx.Next()
	}
}

// patchCSP appends origins to the img-src directive and adds a frame-ancestors
// directive (or extends an existing one) in the given CSP string.
func patchCSP(csp string, origins []string) string {
	suffix := " " + strings.Join(origins, " ")
	directives := strings.Split(csp, ";")
	hasFrameAncestors := false

	for i, d := range directives {
		trimmed := strings.TrimSpace(d)
		switch {
		case strings.HasPrefix(trimmed, "img-src "):
			directives[i] = strings.Replace(d, trimmed, trimmed+suffix, 1)
		case strings.HasPrefix(trimmed, "frame-ancestors "):
			directives[i] = strings.Replace(d, trimmed, trimmed+suffix, 1)
			hasFrameAncestors = true
		}
	}

	result := strings.Join(directives, ";")
	if !hasFrameAncestors {
		result = strings.TrimRight(result, " ;") + "; frame-ancestors 'self'" + suffix + ";"
	}
	return result
}

// Security adds standard security headers to the response.
func Security(isDev bool, cfg ...SecurityConfig) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// 1. Generate a unique nonce for THIS request
		nonce, err := generateNonce()
		if err != nil {
			_ = ctx.AbortWithError(500, err)
			return
		}

		// 2. Store it in the context so your templates can reach it
		ctx.Set("nonce", nonce)

		// 1. Build script-src
		scriptSrc := fmt.Sprintf("'self' 'nonce-%s'", nonce)
		// 2. Build connect-src (Vite uses WebSockets for HMR)
		connectSrc := "'self'"

		if isDev {
			// In CSP3, 'unsafe-inline' is ignored when a nonce/hash is also present.
			// Dev mode needs relaxed inline script support for Vite/HMR, so omit the
			// nonce source from script-src while still exposing the per-request nonce
			// to templates for environments that choose to use it.
			scriptSrc = "'self' 'unsafe-inline' http://localhost:5173 http://127.0.0.1:5173"
			connectSrc += " ws://localhost:5173 ws://127.0.0.1:5173 http://localhost:5173 http://127.0.0.1:5173"
		}

		csp := fmt.Sprintf(
			"default-src 'self'; script-src %s; connect-src %s; style-src 'self' 'unsafe-inline'; img-src 'self' data:;",
			scriptSrc,
			connectSrc,
		)

		if len(cfg) > 0 && cfg[0].ContentSecurityPolicy != "" {
			csp = cfg[0].ContentSecurityPolicy
		}

		ctx.Header("X-Frame-Options", "DENY")
		ctx.Header("X-Content-Type-Options", "nosniff")
		ctx.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		ctx.Header("Content-Security-Policy", csp)
		// HSTS must only be sent over HTTPS (RFC 6797 §7.2).
		isHTTPS := ctx.Request.TLS != nil ||
			(len(cfg) > 0 && cfg[0].TrustProxy && ctx.GetHeader("X-Forwarded-Proto") == "https")
		if isHTTPS {
			ctx.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}
		ctx.Next()
	}
}
