package middlewares

import (
	"github.com/gin-gonic/gin"
)

type SecurityConfig struct {
	ContentSecurityPolicy string // if empty, use a safe default
}

// Security adds standard security headers to the response.
func Security(cfg ...SecurityConfig) gin.HandlerFunc {
	csp := "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:;"
	if len(cfg) > 0 && cfg[0].ContentSecurityPolicy != "" {
		csp = cfg[0].ContentSecurityPolicy
	}
	return func(ctx *gin.Context) {
		ctx.Header("X-Frame-Options", "DENY")
		ctx.Header("X-Content-Type-Options", "nosniff")
		ctx.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		ctx.Header("Content-Security-Policy", csp)
		ctx.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		ctx.Next()
	}
}
