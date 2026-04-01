package product

import (
	"context"
	"fmt"
	"testing"

	"github.com/wssto2/go-core/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProductRepository_TransactionRollbackDoesNotPersist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Create a simple table to use as a marker for transactional writes.
	if err := db.Exec("CREATE TABLE tx_marker (id INTEGER PRIMARY KEY AUTOINCREMENT, val TEXT)").Error; err != nil {
		t.Fatalf("create table: %v", err)
	}

	repo := NewRepository(db)
	transactor := database.NewTransactor(db)

	// Insert inside a transaction and force an error to trigger rollback.
	if err := transactor.WithinTransaction(context.Background(), func(ctx context.Context) error {
		// The concrete gormRepository exposes db(ctx) which honours the transaction
		if rr, ok := repo.(*gormRepository); ok {
			if err := rr.db(ctx).Exec("INSERT INTO tx_marker (val) VALUES (?)", "t1").Error; err != nil {
				return err
			}
			return fmt.Errorf("force rollback")
		}
		return fmt.Errorf("repo is not a gormRepository")
	}); err == nil {
		t.Fatalf("expected error causing rollback, got nil")
	}

	// Ensure the insert was rolled back (no rows present).
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM tx_marker").Scan(&count).Error; err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after rollback, got %d", count)
	}
}
