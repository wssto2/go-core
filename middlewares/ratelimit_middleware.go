package middlewares

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
	rl "github.com/wssto2/go-core/ratelimit"
)

// RateLimit returns a Gin middleware that enforces the provided Limiter.
// It supports three scopes:
//   - global: applies to all requests (always checked)
//   - perUser: when enabled, applies limits per authenticated user (falls back to client IP when unauthenticated)
//   - perEndpoint: when enabled, applies limits per endpoint (method + route full path)
func RateLimit(l rl.Limiter, perUser bool, perEndpoint bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// build keys to check. Semantics:
		// - if both perUser and perEndpoint are enabled, use a composite "user:ID|endpoint:METHOD:PATH" key
		// - otherwise prefer per-user or per-endpoint keys when enabled
		// - when none enabled, use a global key
		keys := make([]string, 0, 2)
		if perUser && perEndpoint {
			// attempt to build a user+endpoint composite key
			id := ""
			if u, ok := auth.UserFromContext(ctx.Request.Context()); ok {
				id = strconv.Itoa(u.GetID())
			} else {
				// Fall back to client IP when no authenticated user is present.
				id = ctx.ClientIP()
			}
			path := ctx.FullPath()
			if path == "" {
				path = ctx.Request.URL.Path
			}
			keys = append(keys, "user:"+id+"|endpoint:"+ctx.Request.Method+":"+path)
		} else if perUser {
			if u, ok := auth.UserFromContext(ctx.Request.Context()); ok {
				keys = append(keys, "user:"+strconv.Itoa(u.GetID()))
			} else {
				// Fall back to client IP when no authenticated user is present.
				keys = append(keys, "user:"+ctx.ClientIP())
			}
		} else if perEndpoint {
			path := ctx.FullPath()
			if path == "" {
				path = ctx.Request.URL.Path
			}
			keys = append(keys, "endpoint:"+ctx.Request.Method+":"+path)
		} else {
			keys = append(keys, "global")
		}

		// evaluate limiter for each key
		for _, k := range keys {
			ok, err := l.Allow(ctx.Request.Context(), k)
			if err != nil {
				// record internal error and return 500
				_ = ctx.Error(apperr.Internal(err))
				ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"error":   "rate limiter internal error",
				})
				return
			}
			if !ok {
				// limit exceeded
				ctx.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"success": false,
					"error":   "rate limit exceeded",
				})
				return
			}
		}

		ctx.Next()
	}
}

// IPRateLimit limits requests per client IP, counting only paths that start
// with pathPrefix (all paths when it is empty).
//
// It is the anonymous-traffic guard next to the per-user limit enforced after
// authentication (auth.DeferUserRateLimit): it stops one machine flooding the
// API or hammering the login endpoint. Scoped to the API prefix, it leaves the
// SPA shell and static assets alone, so a limited user is never handed a JSON
// error body in place of the page. Its ceiling must allow for everyone behind
// one NAT.
func IPRateLimit(l rl.Limiter, pathPrefix string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if pathPrefix != "" && !strings.HasPrefix(ctx.Request.URL.Path, pathPrefix) {
			ctx.Next()

			return
		}

		ok, err := l.Allow(ctx.Request.Context(), "ip:"+ctx.ClientIP())
		if err != nil {
			_ = ctx.Error(apperr.Internal(err))
			ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "error": "rate limiter internal error"})

			return
		}

		if !ok {
			ctx.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"success": false, "error": "rate limit exceeded"})

			return
		}

		ctx.Next()
	}
}
