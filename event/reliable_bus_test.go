package event

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeFailingBus is a small helper for tests: it fails the first N publishes
// and then succeeds.
type fakeFailingBus struct {
	mu        sync.Mutex
	calls     int
	failUntil int
}

func (f *fakeFailingBus) Publish(ctx context.Context, event any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failUntil {
		return assert.AnError // simple non-nil error
	}
	return nil
}

func (f *fakeFailingBus) Subscribe(event any, handler func(ctx context.Context, event any) error) error {
	return nil
}

func TestRetryBus_MovesToDLQWhenExhausted(t *testing.T) {
	t.Parallel()
	f := &fakeFailingBus{failUntil: 5}
	dlq := NewInMemoryDLQ()
	rb, err := NewRetryBus(f, dlq, 3, 1*time.Millisecond)
	assert.NoError(t, err)

	e := struct{ Name string }{Name: "alice"}
	err = rb.Publish(context.Background(), e)
	assert.Error(t, err)

	// retries exhausted
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	assert.Equal(t, 3, calls)

	entries := dlq.Entries()
	assert.Len(t, entries, 1)
	entry := entries[0]
	assert.Equal(t, reflect.TypeOf(e).String(), entry.Subject)

	// entry data should contain the payload (either envelope or raw)
	var env Envelope
	if json.Unmarshal(entry.Data, &env) == nil && len(env.Payload) > 0 {
		var payload struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(env.Payload, &payload)
		assert.Equal(t, "alice", payload.Name)
	} else {
		var payload struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(entry.Data, &payload)
		assert.Equal(t, "alice", payload.Name)
	}
}

func TestRetryBus_SucceedsBeforeExhaust(t *testing.T) {
	t.Parallel()
	f := &fakeFailingBus{failUntil: 1}
	dlq := NewInMemoryDLQ()
	rb, err := NewRetryBus(f, dlq, 3, 1*time.Millisecond)
	assert.NoError(t, err)

	e := struct{ Name string }{Name: "bob"}
	err = rb.Publish(context.Background(), e)
	assert.NoError(t, err)

	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	// expected: one failure then one success
	assert.Equal(t, 2, calls)
	assert.Len(t, dlq.Entries(), 0)
}
