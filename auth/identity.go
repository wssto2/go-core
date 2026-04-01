package auth

import "context"

// Identifiable is the interface for any entity that can be authenticated.
type Identifiable interface {
	GetID() int
}

// IdentityResolver is a function type that finds a user by their ID.
type IdentityResolver func(ctx context.Context, id string) (Identifiable, error)
