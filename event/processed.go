package event

import (
	"context"
	"sync"
	"time"
)

// ctxKey used for storing event data in contexts within this package.
type ctxKey string

const eventRequestIDKey ctxKey = "event.request_id"

// EventIDFromContext retrieves the event request id from context, if present.
func EventIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(eventRequestIDKey).(string)
	return v, ok && v != ""
}

// ProcessedStore stores processed/reserved event IDs to allow idempotent processing.
type ProcessedStore interface {
	// Reserve attempts to reserve the given id. Returns true if reservation was
	// successful (id was not previously reserved/processed). Returns false when
	// the id was already reserved/processed.
	Reserve(ctx context.Context, id string) (bool, error)
	// Confirm marks the id as successfully processed.
	Confirm(ctx context.Context, id string) error
	// Release removes a reservation (used when processing fails and the id
	// should be retried later).
	Release(ctx context.Context, id string) error
}

// InMemoryProcessedStore is a minimal in-memory implementation of ProcessedStore.
// It is suitable for testing and low-volume usage.
type InMemoryProcessedStore struct {
	mu      sync.Mutex
	entries map[string]time.Time // zero time == reserved but not confirmed
	ttl     time.Duration
}

// NewInMemoryProcessedStore constructs a new in-memory store.
func NewInMemoryProcessedStore() *InMemoryProcessedStore {
	return NewInMemoryProcessedStoreWithTTL(24 * time.Hour)
}

// NewInMemoryProcessedStoreWithTTL creates a store that evicts confirmed entries
// older than ttl. A ttl of 0 disables eviction.
func NewInMemoryProcessedStoreWithTTL(ttl time.Duration) *InMemoryProcessedStore {
	return &InMemoryProcessedStore{
		entries: make(map[string]time.Time),
		ttl:     ttl,
	}
}

// Reserve attempts to reserve the id atomically.
func (s *InMemoryProcessedStore) Reserve(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpired()
	if _, ok := s.entries[id]; ok {
		return false, nil
	}
	s.entries[id] = time.Time{}
	return true, nil
}

// Confirm marks the id as processed (sets timestamp).
func (s *InMemoryProcessedStore) Confirm(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	s.entries[id] = time.Now().UTC()
	s.mu.Unlock()
	return nil
}

// Release removes the reservation so the id can be retried later.
func (s *InMemoryProcessedStore) Release(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	delete(s.entries, id)
	s.mu.Unlock()
	return nil
}

// evictExpired deletes entries that have been confirmed and are older than ttl.
// Must be called with s.mu held.
func (s *InMemoryProcessedStore) evictExpired() {
	if s.ttl <= 0 {
		return
	}
	now := time.Now()
	for id, confirmedAt := range s.entries {
		if confirmedAt.IsZero() {
			continue // not yet confirmed; keep it
		}
		if now.Sub(confirmedAt) > s.ttl {
			delete(s.entries, id)
		}
	}
}

// IdempotentHandler wraps a handler so that an event with the same id (from
// context) is processed at most once. Reservation is attempted before the
// handler runs; reservation is released if handler returns error.
func IdempotentHandler(store ProcessedStore, handler func(ctx context.Context, event any) error) func(ctx context.Context, event any) error {
	return func(ctx context.Context, event any) error {
		id, ok := EventIDFromContext(ctx)
		if !ok || id == "" {
			// no id => cannot dedupe
			return handler(ctx, event)
		}
		reserved, err := store.Reserve(ctx, id)
		if err != nil {
			return err
		}
		if !reserved {
			// already processed/reserved
			return nil
		}
		// ensure reservation released on failure
		if err := handler(ctx, event); err != nil {
			_ = store.Release(ctx, id)
			return err
		}
		_ = store.Confirm(ctx, id)
		return nil
	}
}
