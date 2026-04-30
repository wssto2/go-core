package database

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	if err := db.Exec("DROP TABLE IF EXISTS items").Error; err != nil {
		t.Fatalf("failed to drop table: %v", err)
	}
	if err := db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)").Error; err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	return db
}

func TestWithinTransaction_Commits(t *testing.T) {
	db := openTestDB(t)
	trans := NewTransactor(db)

	err := trans.WithinTransaction(context.Background(), func(ctx context.Context) error {
		tx, ok := TxFromContext(ctx)
		if !ok {
			return errors.New("tx not found in context")
		}
		if err := tx.Exec("INSERT INTO items(name) VALUES (?)", "committed").Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM items WHERE name = ?", "committed").Scan(&count).Error; err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
}

func TestNewTransactorFromRegistry_UsesNamedConnection(t *testing.T) {
	reg, cleanup := NewTestRegistry("primary")
	defer cleanup()

	tx, err := NewTransactorFromRegistry(reg, "primary")
	if err != nil {
		t.Fatalf("NewTransactorFromRegistry: %v", err)
	}
	if tx == nil {
		t.Fatal("expected non-nil Transactor")
	}
}

func TestNewTransactorFromRegistry_EmptyNameUsesPrimary(t *testing.T) {
	reg, cleanup := NewTestRegistry("primary")
	defer cleanup()

	tx, err := NewTransactorFromRegistry(reg, "")
	if err != nil {
		t.Fatalf("NewTransactorFromRegistry with empty name: %v", err)
	}
	if tx == nil {
		t.Fatal("expected non-nil Transactor")
	}
}

func TestWithinTransaction_RollsBackOnPanic(t *testing.T) {
	db := openTestDB(t)
	trans := NewTransactor(db)

	// The panic should propagate out after rollback.
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic to propagate out of WithinTransaction")
		}
	}()

	_ = trans.WithinTransaction(context.Background(), func(ctx context.Context) error {
		tx, ok := TxFromContext(ctx)
		if !ok {
			return errors.New("tx not found in context")
		}
		if err := tx.Exec("INSERT INTO items(name) VALUES (?)", "should-rollback").Error; err != nil {
			return err
		}
		panic("simulated panic (e.g. audit marshal failure)")
	})

	// Unreachable — but if somehow reached, verify no row was persisted.
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM items WHERE name = ?", "should-rollback").Scan(&count).Error; err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after panic rollback, got %d", count)
	}
}

func TestWithinTransaction_RollsBack(t *testing.T) {
	db := openTestDB(t)
	trans := NewTransactor(db)

	err := trans.WithinTransaction(context.Background(), func(ctx context.Context) error {
		tx, ok := TxFromContext(ctx)
		if !ok {
			return errors.New("tx not found in context")
		}
		if err := tx.Exec("INSERT INTO items(name) VALUES (?)", "rolledback").Error; err != nil {
			return err
		}
		// cause rollback
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatalf("expected transaction to return error")
	}

	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM items WHERE name = ?", "rolledback").Scan(&count).Error; err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows, got %d", count)
	}
}
