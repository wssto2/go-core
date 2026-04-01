package credentials

import "context"

// Credentials holds access information for storage providers.
type Credentials struct {
	Provider string            // provider name (e.g., s3, gcs)
	Key      string            // access key / id
	Secret   string            // secret or token
	Endpoint string            // optional endpoint URL
	Extra    map[string]string // provider-specific extras
}

// Resolver resolves credentials by name.
// Implementations may fetch from vaults, env, or DB.
type Resolver interface {
	Resolve(ctx context.Context, name string) (*Credentials, error)
}
