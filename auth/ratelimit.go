package auth

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/ratelimit"
)

const userRateLimiterKey = "go-core.auth.user_rate_limiter"

// DeferUserRateLimit arms Authenticated to rate-limit each authenticated user.
//
// A limiter registered on the engine runs before any route group's
// Authenticated middleware, so it cannot know the user yet: keyed "per user", it
// fell back to the client IP for every request, and all users behind one NAT
// (an office) shared one bucket. This middleware only hands the limiter down;
// Authenticated enforces it right after the identity is resolved, keyed by the
// user's ID. Routes without Authenticated are not counted — pair it with a per-IP
// limiter (bootstrap WithIPRateLimit) for anonymous traffic.
func DeferUserRateLimit(l ratelimit.Limiter) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Set(userRateLimiterKey, l)
		ctx.Next()
	}
}

// RateLimitKeyer is implemented by an identity whose user ID does not tell it
// apart, such as a service account that is not a row of the users table: its
// requests are counted under RateLimitKey instead of "user:<id>", so each one
// gets its own bucket.
type RateLimitKeyer interface {
	RateLimitKey() string
}

// rateLimitKey is the bucket an identity's requests are counted in.
func rateLimitKey(user Identifiable) string {
	if k, ok := user.(RateLimitKeyer); ok {
		if key := k.RateLimitKey(); key != "" {
			return key
		}
	}

	return "user:" + strconv.Itoa(user.GetID())
}

// enforceUserRateLimit applies the limiter DeferUserRateLimit armed, if any.
// It reports whether the request may continue; otherwise the response is sent.
func enforceUserRateLimit(ctx *gin.Context, user Identifiable) bool {
	value, ok := ctx.Get(userRateLimiterKey)
	if !ok {
		return true
	}

	limiter, ok := value.(ratelimit.Limiter)
	if !ok || limiter == nil {
		return true
	}

	allowed, err := limiter.Allow(ctx.Request.Context(), rateLimitKey(user))
	if err != nil {
		_ = ctx.Error(apperr.Internal(err))
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "error": "rate limiter internal error"})

		return false
	}

	if !allowed {
		ctx.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"success": false, "error": "rate limit exceeded"})

		return false
	}

	return true
}
