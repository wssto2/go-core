package middleware

// DEPRECATED: this package previously exposed an auth.Resolver compatible
// with an older version of the auth package. The current example uses the
// simpler auth.IdentityResolver (in internal/domain/auth) and the
// AuthProvider built by bootstrap.WithJWTAuth. Keep this file as a stub to
// avoid accidental re-use.

// See internal/domain/auth/identity.go for the example IdentityResolver used
// by the example application.
