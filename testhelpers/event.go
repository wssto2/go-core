package testhelpers

import (
	"context"

	"github.com/wssto2/go-core/event"
)

// NewInMemoryBus returns a ready-to-use in-memory event bus for tests.
func NewInMemoryBus() *event.InMemoryBus {
	b := event.NewInMemoryBus()
	return b
}

// PublishSync publishes an event and waits for handlers to complete. This is a
// small helper that simply calls bus.Publish and returns its result.
func PublishSync(ctx context.Context, b event.Bus, ev any) error {
	return b.Publish(ctx, ev)
}
