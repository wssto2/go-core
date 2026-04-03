package event

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSubscription is a test double for Subscription.
type mockSubscription struct {
	unsubscribed atomic.Bool
}

func (s *mockSubscription) Unsubscribe() error {
	s.unsubscribed.Store(true)
	return nil
}

// mockNatsClient allows tests to control Subscribe/Publish without a real NATS server.
type mockNatsClient struct {
	handlers map[string]func([]byte)
	subs     map[string]*mockSubscription
}

func newMockNatsClient() *mockNatsClient {
	return &mockNatsClient{
		handlers: make(map[string]func([]byte)),
		subs:     make(map[string]*mockSubscription),
	}
}

func (c *mockNatsClient) Publish(subject string, data []byte) error {
	if h, ok := c.handlers[subject]; ok {
		h(data)
	}
	return nil
}

func (c *mockNatsClient) Subscribe(subject string, handler func([]byte)) (Subscription, error) {
	sub := &mockSubscription{}
	c.handlers[subject] = handler
	c.subs[subject] = sub
	return sub, nil
}

// natsSubject returns the NATS subject NATSBus uses for the given value (mirrors reflect.TypeOf(v).String()).
func natsSubject(v any) string {
	return reflect.TypeOf(v).String()
}

// TestNATSBus_Resubscribe_NoDuplicateHandlers verifies that a second Subscribe
// call for the same subject replaces the first subscription (unsubscribes it)
// and the handler is invoked exactly once per message.
func TestNATSBus_Resubscribe_NoDuplicateHandlers(t *testing.T) {
	client := newMockNatsClient()
	bus := NewNATSBus(client, nil)

	type testEvent struct{ Value int }

	var callCount atomic.Int32
	handler := func(ctx context.Context, e any) error {
		callCount.Add(1)
		return nil
	}

	// First subscription.
	require.NoError(t, bus.Subscribe(testEvent{}, handler))
	subject := natsSubject(testEvent{})
	firstSub := client.subs[subject]
	require.NotNil(t, firstSub)

	// Second subscription for the same subject — must replace the first.
	require.NoError(t, bus.Subscribe(testEvent{}, handler))

	assert.True(t, firstSub.unsubscribed.Load(), "first subscription must be unsubscribed on re-registration")

	// Deliver a raw event to the current handler (non-envelope path for simplicity).
	require.NoError(t, client.Publish(subject, []byte(`{"Value":42}`)))

	assert.Equal(t, int32(1), callCount.Load(), "handler must be invoked exactly once per message")
}

// TestNATSBus_Close_UnsubscribesAll verifies that Close unsubscribes all tracked subscriptions.
func TestNATSBus_Close_UnsubscribesAll(t *testing.T) {
	type evA struct{}
	type evB struct{}

	client := newMockNatsClient()
	bus := NewNATSBus(client, nil)

	noop := func(ctx context.Context, e any) error { return nil }
	require.NoError(t, bus.Subscribe(evA{}, noop))
	require.NoError(t, bus.Subscribe(evB{}, noop))

	require.NoError(t, bus.Close())

	for subject, sub := range client.subs {
		assert.True(t, sub.unsubscribed.Load(), "subscription for %s must be unsubscribed after Close", subject)
	}
}
