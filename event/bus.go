package event

import (
	"context"
	"reflect"
	"sync"
)

type Bus interface {
	Publish(ctx context.Context, event any) error
	Subscribe(event any, handler func(ctx context.Context, event any) error) error
}

// InMemoryBus is an in-memory implementation of the Bus interface.
type InMemoryBus struct {
	mu       sync.RWMutex
	handlers map[reflect.Type][]func(ctx context.Context, event any) error
}

func NewInMemoryBus() *InMemoryBus {
	return &InMemoryBus{
		handlers: make(map[reflect.Type][]func(ctx context.Context, event any) error),
	}
}

func (b *InMemoryBus) Publish(ctx context.Context, event any) error {
	b.mu.RLock()
	handlers := b.handlers[reflect.TypeOf(event)]
	b.mu.RUnlock()
	for _, h := range handlers {
		if err := h(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (b *InMemoryBus) Subscribe(event any, handler func(ctx context.Context, event any) error) error {
	t := reflect.TypeOf(event)
	b.mu.Lock()
	b.handlers[t] = append(b.handlers[t], handler)
	b.mu.Unlock()
	return nil
}
