package auth

import "context"

// Provider verifies a token and returns the Identifiable user it stands for.
type Provider interface {
	// Verify takes a raw string (from Header/CLI) and returns the User.
	Verify(ctx context.Context, token string) (Identifiable, error)
}
