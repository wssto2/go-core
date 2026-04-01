package bootstrap

import (
	"context"
)

// Module is implemented by every domain package that wants to register
// its own dependencies and routes.
type Module interface {
	Name() string
	Register(c *Container) error
	Boot(ctx context.Context) error
	Shutdown(ctx context.Context) error
}
