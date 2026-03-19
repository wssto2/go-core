package auth

import "fmt"

// Policy represents a permission check — a namespace and an action.
//
// Example:
//
//	GeneratePolicy("customers.customers", "view")
//	→ Policy{Namespace: "customers.customers", Action: "view"}
type Policy struct {
	Namespace string
	Action    string
}

// String returns the policy in "namespace:action" format for logging.
func (p Policy) String() string {
	return fmt.Sprintf("%s:%s", p.Namespace, p.Action)
}

// GeneratePolicy constructs a Policy from a namespace and action.
// This matches your existing auth.GeneratePolicy(namespace, "view") call pattern.
func GeneratePolicy(namespace, action string) Policy {
	return Policy{
		Namespace: namespace,
		Action:    action,
	}
}

// IsAuthorized checks whether the given user is authorized for the policy.
// The default implementation checks roles in the format "namespace:action".
// Applications can replace this with a more sophisticated check (e.g. Casbin)
// by providing a custom Authorizer.
func IsAuthorized(user Identifiable, policy Policy) bool {
	// Super-admin bypasses all policy checks
	if user.HasRole("super-admin") {
		return true
	}

	// Check exact role match: "customers.customers:view"
	required := policy.String()
	if user.HasRole(required) {
		return true
	}

	// Check wildcard namespace match: "customers.customers:*"
	wildcard := fmt.Sprintf("%s:*", policy.Namespace)
	if user.HasRole(wildcard) {
		return true
	}

	return false
}

// Authorizer is a function type that determines whether a user can perform
// a policy action. Applications can swap this out for Casbin, OPA, etc.
type Authorizer func(user Identifiable, policy Policy) bool

// DefaultAuthorizer uses role-based checks as defined in IsAuthorized.
var DefaultAuthorizer Authorizer = IsAuthorized
