package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
)

// Authorize returns a middleware that checks if the authenticated user has
// the given policy. The user must implement either HasPolicy(string) bool
// or GetPolicies() []string.
func Authorize(policy Policy) gin.HandlerFunc {
	authorizer := func(user Identifiable, p Policy) bool {
		return hasPolicy(user, p)
	}
	return AuthorizedWith(policy, authorizer)
}

// AuthorizeAny returns a middleware that grants access if the authenticated
// user holds at least one of the given policies (OR logic). Use Authorize for
// a single policy; chain multiple Authorize calls when ALL policies are required.
func AuthorizeAny(policies ...Policy) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		user, ok := GetIdentifiable(ctx)
		if !ok {
			_ = ctx.Error(apperr.Unauthorized("user not authenticated"))
			ctx.Abort()
			return
		}

		for _, p := range policies {
			if hasPolicy(user, p) {
				ctx.Next()
				return
			}
		}

		_ = ctx.Error(apperr.Forbidden("access denied"))
		ctx.Abort()
	}
}

// hasPolicy reports whether user holds the given policy.
func hasPolicy(user Identifiable, p Policy) bool {
	type withHasPolicy interface {
		HasPolicy(string) bool
	}
	if hp, ok := user.(withHasPolicy); ok {
		return hp.HasPolicy(p.String())
	}
	type withGetPolicies interface {
		GetPolicies() []string
	}
	if gp, ok := user.(withGetPolicies); ok {
		for _, pol := range gp.GetPolicies() {
			if pol == p.String() {
				return true
			}
		}
	}
	return false
}
