package middlewares

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/gin-gonic/gin"
)

type SecurityConfig struct {
	ContentSecurityPolicy string // if empty, use a safe default
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
			// Allow Vite Dev Server
			scriptSrc += " localhost:5173"
			connectSrc += " ws://localhost:5173 localhost:5173"
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
		ctx.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		ctx.Next()
	}
}
