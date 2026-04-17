package middlewares

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

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
