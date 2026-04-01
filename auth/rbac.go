package auth

import "github.com/gin-gonic/gin"

// Authorize returns a middleware that checks if the authenticated user has
// the given policy. The user must implement either HasPolicy(string) bool
// or GetPolicies() []string.
func Authorize(policy Policy) gin.HandlerFunc {
	authorizer := func(user Identifiable, p Policy) bool {
		type hasPolicy interface {
			HasPolicy(string) bool
		}
		if hp, ok := user.(hasPolicy); ok {
			return hp.HasPolicy(p.String())
		}
		type getPolicies interface {
			GetPolicies() []string
		}
		if gp, ok := user.(getPolicies); ok {
			for _, pol := range gp.GetPolicies() {
				if pol == p.String() {
					return true
				}
			}
		}
		return false
	}
	return AuthorizedWith(policy, authorizer)
}
