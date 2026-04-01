package middlewares

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type CorsConfig struct {
	AllowOrigins []string
	AllowMethods []string
	AllowHeaders []string
}

func Cors(cfg CorsConfig) gin.HandlerFunc {
	isWildcard := len(cfg.AllowOrigins) == 1 && cfg.AllowOrigins[0] == "*"

	allowedSet := make(map[string]bool, len(cfg.AllowOrigins))
	for _, o := range cfg.AllowOrigins {
		allowedSet[o] = true
	}

	return func(ctx *gin.Context) {
		origin := ctx.GetHeader("Origin")
		if origin == "" {
			ctx.Next()
			return
		}

		if ctx.Request.Method == http.MethodOptions {
			if isWildcard {
				ctx.Header("Access-Control-Allow-Origin", "*")
			} else if allowedSet[origin] {
				ctx.Header("Access-Control-Allow-Origin", origin)
				ctx.Header("Access-Control-Allow-Credentials", "true")
				ctx.Header("Vary", "Origin")
			}
			ctx.Header("Access-Control-Allow-Methods", strings.Join(cfg.AllowMethods, ", "))
			ctx.Header("Access-Control-Allow-Headers", strings.Join(cfg.AllowHeaders, ", "))
			ctx.AbortWithStatus(http.StatusNoContent)
			return
		}

		if isWildcard {
			// Wildcard origin: cannot be combined with credentials per CORS spec
			ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			// Do NOT set Allow-Credentials here
		} else if allowedSet[origin] {
			// Specific origin match: credentials are safe
			ctx.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			ctx.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			ctx.Writer.Header().Add("Vary", "Origin") // required for CDN caching correctness
		}

		ctx.Next()
	}
}
