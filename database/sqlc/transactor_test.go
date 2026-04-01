package sqlc

import (
	"context"
	"testing"

	"github.com/wssto2/go-core/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTransactor_ProvidesSQLCQuerier(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	trans := database.NewTransactor(db)
	repo := NewSQLCRepository(nil, trans)
	called := false
	if err := repo.WithinTransaction(context.Background(), func(ctx context.Context, q Querier) error {
		if q == nil {
			t.Fatalf("expected querier in context, got nil")
		}
		called = true
		return nil
	}); err != nil {
		t.Fatalf("transaction failed: %v", err)
	}
	if !called {
		t.Fatalf("callback not executed")
	}
}
