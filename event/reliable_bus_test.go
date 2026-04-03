package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestInMemoryDLQ_Overflow_DropsOldest(t *testing.T) {
dlq := NewInMemoryDLQWithSize(3, nil)
ctx := context.Background()
err := errors.New("fail")

for i := 0; i < 5; i++ {
subject := fmt.Sprintf("evt-%d", i)
_ = dlq.Enqueue(ctx, subject, []byte(`{}`), err)
}

assert.Equal(t, int64(2), dlq.Dropped(), "expected 2 dropped entries")

entries := dlq.Entries()
assert.Len(t, entries, 3, "queue must stay at maxSize")
// Oldest 2 entries (evt-0, evt-1) should be gone; remaining are evt-2..evt-4.
assert.Equal(t, "evt-2", entries[0].Subject)
assert.Equal(t, "evt-4", entries[2].Subject)
}
