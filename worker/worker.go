package worker

import (
	"context"
)

// Worker represents a background process that runs alongside the HTTP server.
type Worker interface {
	Name() string
	// Run starts the worker loop. It should block until the context is cancelled.
	Run(ctx context.Context) error
}
