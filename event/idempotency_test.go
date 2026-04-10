package event

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/wssto2/go-core/database"
)

func TestIdempotency_ReserveWithinTransaction(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureIdempotencySchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	trans := database.NewTransactor(db)
	key := "test-key-1"

	// Reserve within transaction and commit
	if err := trans.WithinTransaction(context.Background(), func(ctx context.Context) error {
		tx, ok := database.TxFromContext(ctx)
		if !ok {
			return errors.New("no tx")
		}
		reserved, err := ReserveKey(ctx, tx, key)
		if err != nil {
			return err
		}
		if !reserved {
			return errors.New("expected reserved in tx")
		}
		return nil
	}); err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// attempt to reserve again using db (non-tx) should return not reserved
	reserved, err := ReserveKey(context.Background(), db, key)
	if err != nil {
		t.Fatalf("reserve second: %v", err)
	}
	if reserved {
		t.Fatalf("expected not reserved after already present")
	}
}

func TestIdempotency_ReserveRollback(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureIdempotencySchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	trans := database.NewTransactor(db)
	key := "rollback-key"

	err := trans.WithinTransaction(context.Background(), func(ctx context.Context) error {
		tx, ok := database.TxFromContext(ctx)
		if !ok {
			return errors.New("no tx")
		}
		reserved, err := ReserveKey(ctx, tx, key)
		if err != nil {
			return err
		}
		if !reserved {
			return errors.New("expected reserved in tx")
		}
		// force rollback
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatalf("expected forced error")
	}

	// After rollback, reservation should not be present
	reserved, err := ReserveKey(context.Background(), db, key)
	if err != nil {
		t.Fatalf("reserve after rollback: %v", err)
	}
	if !reserved {
		t.Fatalf("expected reserved after rollback (previous reservation rolled back)")
	}
}

func TestIdempotency_ReserveEmptyKey(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureIdempotencySchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	reserved, err := ReserveKey(context.Background(), db, "")
	if err != nil {
		t.Fatalf("reserve empty: %v", err)
	}
	if reserved {
		t.Fatalf("expected not reserved for empty key")
	}
}

func TestDBProcessedStore_ReserveConfirmRelease(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureIdempotencySchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	store := NewDBProcessedStore(db)
	ctx := context.Background()

	ok, err := store.Reserve(ctx, "db-id-1")
	assert.NoError(t, err)
	assert.True(t, ok, "first reservation should succeed")

	ok, err = store.Reserve(ctx, "db-id-1")
	assert.NoError(t, err)
	assert.False(t, ok, "duplicate reservation should fail")

	assert.NoError(t, store.Confirm(ctx, "db-id-1"))

	assert.NoError(t, store.Release(ctx, "db-id-1"))

	ok, err = store.Reserve(ctx, "db-id-1")
	assert.NoError(t, err)
	assert.True(t, ok, "should be reservable after release")
}

func TestDBProcessedStore_ImplementsProcessedStore(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureIdempotencySchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	var _ ProcessedStore = NewDBProcessedStore(db)
}

func TestDBProcessedStore_IdempotentHandler(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureIdempotencySchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	store := NewDBProcessedStore(db)
	calls := 0
	handler := func(ctx context.Context, event any) error {
		calls++
		return nil
	}
	wrapped := IdempotentHandler(store, handler)
	ctx := context.WithValue(context.Background(), eventRequestIDKey, "db-evt-1")

	assert.NoError(t, wrapped(ctx, struct{}{}))
	assert.NoError(t, wrapped(ctx, struct{}{}))
	assert.Equal(t, 1, calls, "handler should be called exactly once")
}

func TestDBProcessedStore_WithinTransaction(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureIdempotencySchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	trans := database.NewTransactor(db)
	ctx := context.Background()

	err := trans.WithinTransaction(ctx, func(ctx context.Context) error {
		tx, ok := database.TxFromContext(ctx)
		if !ok {
			return errors.New("no tx in context")
		}
		store := NewDBProcessedStore(tx)
		ok, err := store.Reserve(ctx, "tx-id-1")
		if err != nil || !ok {
			return errors.New("expected reservation to succeed in tx")
		}
		return errors.New("force rollback")
	})
	assert.Error(t, err)

	// After rollback, the id should be reservable again.
	store := NewDBProcessedStore(db)
	ok, err := store.Reserve(ctx, "tx-id-1")
	assert.NoError(t, err)
	assert.True(t, ok, "should be reservable after rollback")
}

// TestDBProcessedStore_PurgeStaleReservations_RemovesOldEntries confirms that
// PurgeStaleReservations deletes reserved-but-never-confirmed rows while leaving
// recently-reserved and confirmed rows intact.
func TestDBProcessedStore_PurgeStaleReservations_RemovesOldEntries(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureIdempotencySchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	ctx := context.Background()
	store := NewDBProcessedStore(db)

	// Reserve stale key and backdate its created_at via raw SQL.
	ok, err := store.Reserve(ctx, "stale-key")
	assert.NoError(t, err)
	assert.True(t, ok)
	db.Exec("UPDATE idempotency_records SET created_at = datetime('now', '-2 hours') WHERE key = 'stale-key'")

	// Reserve a fresh key (should survive purge).
	ok, err = store.Reserve(ctx, "fresh-key")
	assert.NoError(t, err)
	assert.True(t, ok)

	// Reserve a key and mark it as confirmed (processed_at is set, should survive purge).
	ok, err = store.Reserve(ctx, "confirmed-key")
	assert.NoError(t, err)
	assert.True(t, ok)
	db.Exec("UPDATE idempotency_records SET created_at = datetime('now', '-2 hours'), processed_at = datetime('now') WHERE key = 'confirmed-key'")

	// Purge reservations older than 1 hour.
	assert.NoError(t, store.PurgeStaleReservations(ctx, time.Hour))

	// stale-key must be gone.
	_, foundStale, err := store.GetResponse(ctx, "stale-key")
	assert.NoError(t, err)
	assert.False(t, foundStale, "stale reservation must be deleted")

	// fresh-key must remain.
	_, foundFresh, err := store.GetResponse(ctx, "fresh-key")
	assert.NoError(t, err)
	assert.True(t, foundFresh, "fresh reservation must not be deleted")

	// confirmed-key must remain.
	_, foundConfirmed, err := store.GetResponse(ctx, "confirmed-key")
	assert.NoError(t, err)
	assert.True(t, foundConfirmed, "confirmed entry must not be deleted")
}
