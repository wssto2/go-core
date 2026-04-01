package event

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type myEvent struct{ V int }

func TestSubscribeTypedAndPublishTyped(t *testing.T) {
	b := NewInMemoryBus()
	var got int
	err := SubscribeTyped(b, func(ctx context.Context, e myEvent) error {
		got = e.V
		return nil
	})
	assert.NoError(t, err)

	err = PublishTyped(b, context.Background(), myEvent{V: 7})
	assert.NoError(t, err)
	assert.Equal(t, 7, got)
}

func TestSubscribeTypedWithPublishUntyped(t *testing.T) {
	b := NewInMemoryBus()
	var got int

	err := SubscribeTyped(b, func(ctx context.Context, e myEvent) error {
		got = e.V
		return nil
	})
	assert.NoError(t, err)

	err = b.Publish(context.Background(), myEvent{V: 9})
	assert.NoError(t, err)
	assert.Equal(t, 9, got)
}

func TestSubscribeUntypedPublishTyped(t *testing.T) {
	b := NewInMemoryBus()
	var got int

	err := b.Subscribe(myEvent{}, func(ctx context.Context, e any) error {
		ev := e.(myEvent)
		got = ev.V
		return nil
	})
	assert.NoError(t, err)

	err = PublishTyped(b, context.Background(), myEvent{V: 11})
	assert.NoError(t, err)
	assert.Equal(t, 11, got)
}
