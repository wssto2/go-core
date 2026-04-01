package sqlc

import (
	"context"
	"testing"

	"github.com/wssto2/go-core/database"
)

// fakeTransactor is a minimal Transactor implementation for tests.
type fakeTransactor struct {
	called bool
}

func (f *fakeTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	f.called = true
	return fn(ctx)
}

func TestSQLCRepository_WithinTransaction_CallsTransactor(t *testing.T) {
	f := &fakeTransactor{}
	repo := NewSQLCRepository(&Queries{}, database.Transactor(f))
	err := repo.WithinTransaction(context.Background(), func(ctx context.Context, q Querier) error {
		if q == nil {
			t.Fatal("querier is nil")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !f.called {
		t.Fatalf("transactor not called")
	}
}

func TestSQLCRepository_NoTransactor_ExecutesDirectly(t *testing.T) {
	repo := NewSQLCRepository(&Queries{}, nil)
	called := false
	err := repo.WithinTransaction(context.Background(), func(ctx context.Context, q Querier) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("function not executed")
	}
}
