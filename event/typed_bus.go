package event

import (
	"context"
	"reflect"
)

// SubscribeTyped registers a handler for events of type T.
// This provides a typed API that lets callers publish without using reflect.TypeOf(event)
// at the hot-path publish site (use PublishTyped), reducing runtime reflection in the hot path.
func SubscribeTyped[T any](b *InMemoryBus, handler func(ctx context.Context, event T) error) error {
	t := reflect.TypeFor[T]()
	adapter := func(ctx context.Context, e any) error {
		// Fast path: direct type assertion (no reflection)
		if ev, ok := e.(T); ok {
			return handler(ctx, ev)
		}
		// Types didn't match; do nothing. Keep minimal and avoid heavy reflection here.
		return nil
	}
	b.mu.Lock()
	b.handlers[t] = append(b.handlers[t], adapter)
	b.mu.Unlock()
	return nil
}

// PublishTyped publishes an event of type T using a typed lookup key computed from T.
// Callers that use PublishTyped avoid calling reflect.TypeOf(event) at publish time.
func PublishTyped[T any](b *InMemoryBus, ctx context.Context, event T) error {
	t := reflect.TypeFor[T]()
	b.mu.RLock()
	handlers := b.handlers[t]
	b.mu.RUnlock()
	for _, h := range handlers {
		if err := h(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
