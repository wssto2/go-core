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

// Authorizer is a function type that determines whether a user can perform
// a policy action. Applications can swap this out for Casbin, OPA, etc.
type Authorizer func(user Identifiable, policy Policy) bool
