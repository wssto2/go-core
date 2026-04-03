package event

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/resilience"
)

// DeadLetterEntry represents a stored event that failed delivery after retries.
type DeadLetterEntry struct {
	Subject   string    `json:"subject"`
	Data      []byte    `json:"data"`
	Error     string    `json:"error"`
	Timestamp time.Time `json:"timestamp"`
}

// DeadLetterQueue is a minimal DLQ abstraction. Implementations are responsible
// for durable storage or forwarding of failed messages.
type DeadLetterQueue interface {
	Enqueue(ctx context.Context, subject string, data []byte, cause error) error
}

// InMemoryDLQ is a simple in-memory dead-letter queue suitable for tests and
// low-volume usage. When the queue is at capacity the oldest entry is dropped.
type InMemoryDLQ struct {
	mu      sync.Mutex
	entries []DeadLetterEntry
	maxSize int
	dropped int64
	log     *slog.Logger
}

// NewInMemoryDLQ constructs an unbounded in-memory DLQ (maxSize=0).
// Prefer NewInMemoryDLQWithSize for production use.
func NewInMemoryDLQ() *InMemoryDLQ { return &InMemoryDLQ{} }

// NewInMemoryDLQWithSize constructs a bounded DLQ. When len(entries) >= maxSize,
// the oldest entry is evicted and Dropped() is incremented. maxSize <= 0 means
// unbounded.
func NewInMemoryDLQWithSize(maxSize int, log *slog.Logger) *InMemoryDLQ {
	return &InMemoryDLQ{maxSize: maxSize, log: log}
}

// Dropped returns the number of entries that were silently evicted due to overflow.
func (d *InMemoryDLQ) Dropped() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dropped
}

// Enqueue stores the failed message into the DLQ.
func (d *InMemoryDLQ) Enqueue(ctx context.Context, subject string, data []byte, cause error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.maxSize > 0 && len(d.entries) >= d.maxSize {
		d.entries = d.entries[1:] // drop oldest
		d.dropped++
		if d.log != nil {
			d.log.Warn("dlq: capacity exceeded, oldest entry dropped", "max_size", d.maxSize, "dropped_total", d.dropped)
		}
	}
	entry := DeadLetterEntry{
		Subject:   subject,
		Data:      append([]byte(nil), data...), // copy
		Error:     cause.Error(),
		Timestamp: time.Now().UTC(),
	}
	d.entries = append(d.entries, entry)
	return nil
}

// Entries returns a copy of stored entries.
func (d *InMemoryDLQ) Entries() []DeadLetterEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]DeadLetterEntry, len(d.entries))
	copy(out, d.entries)
	return out
}

// RetryBus wraps another Bus and adds retry semantics. When all retries are
// exhausted the event is moved to the provided DeadLetterQueue.
type RetryBus struct {
	bus            Bus
	dlq            DeadLetterQueue
	attempts       int
	initialBackoff time.Duration
}

// NewRetryBus constructs a RetryBus. attempts must be >= 1 and dlq must
// be non-nil. If initialBackoff <= 0 a sensible default is used.
func NewRetryBus(bus Bus, dlq DeadLetterQueue, attempts int, initialBackoff time.Duration) (*RetryBus, error) {
	if attempts < 1 {
		return nil, apperr.BadRequest("attempts must be >= 1")
	}
	if dlq == nil {
		return nil, apperr.BadRequest("dlq is nil")
	}
	if initialBackoff <= 0 {
		initialBackoff = 100 * time.Millisecond
	}
	return &RetryBus{bus: bus, dlq: dlq, attempts: attempts, initialBackoff: initialBackoff}, nil
}

// Publish attempts to publish the event using the wrapped bus. If all attempts
// fail the event is serialized (envelope when possible) and enqueued to the DLQ.
func (r *RetryBus) Publish(ctx context.Context, event any) error {
	err := resilience.Retry(ctx, r.attempts, r.initialBackoff, func(c context.Context) error {
		return r.bus.Publish(c, event)
	})
	if err == nil {
		return nil
	}

	// Prepare data for DLQ; prefer Envelope so metadata is preserved.
	var data []byte
	if env, e := WrapEventWithMetadata(ctx, event); e == nil {
		if j, jmErr := json.Marshal(env); jmErr == nil {
			data = j
		}
	}
	if len(data) == 0 {
		// Fallback to raw event JSON
		if j, jmErr := json.Marshal(event); jmErr == nil {
			data = j
		}
	}
	if len(data) == 0 {
		// Both serialization attempts failed: enqueuing would create an unrecoverable
		// DLQ entry with no payload. Return the original publish error directly.
		return apperr.Internal(err)
	}

	subject := reflect.TypeOf(event).String()
	// best-effort enqueue; DLQ failures are not fatal for this flow
	_ = r.dlq.Enqueue(ctx, subject, data, err)
	return apperr.Internal(err)
}

// Subscribe delegates to the underlying bus.
func (r *RetryBus) Subscribe(event any, handler func(ctx context.Context, event any) error) error {
	return r.bus.Subscribe(event, handler)
}
