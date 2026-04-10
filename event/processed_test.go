package event

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInMemoryProcessedStore_ReserveConfirmRelease(t *testing.T) {
	s := NewInMemoryProcessedStore()
	ok, err := s.Reserve(context.Background(), "id1")
	assert.NoError(t, err)
	assert.True(t, ok)
	ok, err = s.Reserve(context.Background(), "id1")
	assert.NoError(t, err)
	assert.False(t, ok)
	err = s.Confirm(context.Background(), "id1")
	assert.NoError(t, err)
	// release should remove
	err = s.Release(context.Background(), "id1")
	assert.NoError(t, err)
	ok, _ = s.Reserve(context.Background(), "id1")
	assert.True(t, ok)
}

func TestIdempotentHandler_Dedupes(t *testing.T) {
	store := NewInMemoryProcessedStore()
	var calls int32
	handler := func(ctx context.Context, event any) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}
	wrapped := IdempotentHandler(store, handler)
	ctx := context.WithValue(context.Background(), eventRequestIDKey, "evt-1")
	_ = wrapped(ctx, struct{}{})
	_ = wrapped(ctx, struct{}{})
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestIdempotentHandler_Concurrent(t *testing.T) {
	store := NewInMemoryProcessedStore()
	var calls int32
	handler := func(ctx context.Context, event any) error {
		// extend processing window
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&calls, 1)
		return nil
	}
	wrapped := IdempotentHandler(store, handler)
	ctx := context.WithValue(context.Background(), eventRequestIDKey, "evt-2")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = wrapped(ctx, struct{}{})
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestInMemoryProcessedStore_EvictsExpiredEntries(t *testing.T) {
	ttl := 50 * time.Millisecond
	store := NewInMemoryProcessedStoreWithTTL(ttl)
	ctx := context.Background()

	reserved, err := store.Reserve(ctx, "evt-evict-1")
	if err != nil || !reserved {
		t.Fatalf("Reserve: got reserved=%v err=%v, want true nil", reserved, err)
	}
	if err := store.Confirm(ctx, "evt-evict-1"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	time.Sleep(2 * ttl)

	reserved, err = store.Reserve(ctx, "evt-evict-2")
	if err != nil || !reserved {
		t.Fatalf("Reserve 2: got reserved=%v err=%v, want true nil", reserved, err)
	}

	// The first entry should have been evicted.
	reserved, err = store.Reserve(ctx, "evt-evict-1")
	if err != nil {
		t.Fatalf("Reserve after eviction: %v", err)
	}
	if !reserved {
		t.Errorf("Expected to reserve evicted id again, got reserved=false")
	}
}

func TestIdempotentHandler_ReleaseOnError(t *testing.T) {
	store := NewInMemoryProcessedStore()
	var calls int32
	handler := func(ctx context.Context, event any) error {
		if atomic.AddInt32(&calls, 1) == 1 {
			return assert.AnError
		}
		return nil
	}
	wrapped := IdempotentHandler(store, handler)
	ctx := context.WithValue(context.Background(), eventRequestIDKey, "evt-3")
	err := wrapped(ctx, struct{}{})
	assert.Error(t, err)
	err = wrapped(ctx, struct{}{})
	assert.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestInMemoryProcessedStore_StaleReservationEviction(t *testing.T) {
	s := &InMemoryProcessedStore{
		entries:        make(map[string]time.Time),
		reservedAt:     make(map[string]time.Time),
		ttl:            time.Hour,
		reservationTTL: time.Millisecond, // very short
	}
	ctx := context.Background()

	ok, err := s.Reserve(ctx, "evict-me")
	assert.NoError(t, err)
	assert.True(t, ok)

	// Backdate the reservation to appear stale.
	s.mu.Lock()
	s.reservedAt["evict-me"] = time.Now().Add(-time.Second)
	s.mu.Unlock()

	// Next call to Reserve triggers evictExpired; now "evict-me" should be evictable.
	ok, err = s.Reserve(ctx, "trigger-evict")
	assert.NoError(t, err)
	assert.True(t, ok)

	// "evict-me" was stale so it should have been evicted, allowing re-reservation.
	ok, err = s.Reserve(ctx, "evict-me")
	assert.NoError(t, err)
	assert.True(t, ok, "stale reservation should be evictable and re-reservable")
}
