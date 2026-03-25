package auth

import "context"

// Identifiable is the interface for any entity that can be authenticated.
type Identifiable interface {
	GetID() int
	GetEmail() string
	GetPolicies() []string
	HasPolicy(policy string) bool
	HasAnyPolicy(policy ...string) bool
	HasAllPolicies(policy ...string) bool
}

// IdentityResolver is a function type that finds a user by their ID.
// T is your application's User struct.
type IdentityResolver[T Identifiable] func(ctx context.Context, id string) (T, error)
