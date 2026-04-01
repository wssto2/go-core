package event

import (
	"context"
	"errors"
	"testing"

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
