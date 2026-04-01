package event

import (
	"context"
	"testing"
)

type benchEvent struct{ V int }

func BenchmarkInMemoryBus_Publish_NoHandlers(b *testing.B) {
	bus := NewInMemoryBus()
	ctx := context.Background()
	ev := benchEvent{V: 1}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := bus.Publish(ctx, ev); err != nil {
			b.Fatalf("publish error: %v", err)
		}
	}
}

func BenchmarkInMemoryBus_Publish_WithHandler(b *testing.B) {
	bus := NewInMemoryBus()
	ctx := context.Background()
	ev := benchEvent{V: 1}
	_ = bus.Subscribe(benchEvent{}, func(ctx context.Context, event any) error {
		_ = event.(benchEvent).V
		return nil
	})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := bus.Publish(ctx, ev); err != nil {
			b.Fatalf("publish error: %v", err)
		}
	}
}
