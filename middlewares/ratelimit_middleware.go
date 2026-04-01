package middlewares

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
	rl "github.com/wssto2/go-core/ratelimit"
)

// RateLimit returns a Gin middleware that enforces the provided Limiter.
// It supports three scopes:
//   - global: applies to all requests (always checked)
//   - perUser: when enabled, applies limits per authenticated user (uses auth.UserFromContext or X-User-ID header)
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
			} else if h := ctx.GetHeader("X-User-ID"); h != "" {
				id = h
			}
			path := ctx.FullPath()
			if path == "" {
				path = ctx.Request.URL.Path
			}
			if id != "" {
				keys = append(keys, "user:"+id+"|endpoint:"+ctx.Request.Method+":"+path)
			} else {
				// no user info: fall back to endpoint
				keys = append(keys, "endpoint:"+ctx.Request.Method+":"+path)
			}
		} else if perUser {
			if u, ok := auth.UserFromContext(ctx.Request.Context()); ok {
				keys = append(keys, "user:"+strconv.Itoa(u.GetID()))
			} else if id := ctx.GetHeader("X-User-ID"); id != "" {
				keys = append(keys, "user:"+id)
			} else {
				// no user info -> treat as global
				keys = append(keys, "global")
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
				ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "rate limiter internal error"})
				return
			}
			if !ok {
				// limit exceeded
				ctx.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
				return
			}
		}

		ctx.Next()
	}
}
