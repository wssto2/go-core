package audit

import (
	"context"
	"testing"
)

// fakeTransactor is a minimal Transactor implementation used for testing.
type fakeTransactor struct {
	called bool
}

func (f *fakeTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	f.called = true
	return fn(ctx)
}

func TestRepositoryWriteUsesTransactorAndHook(t *testing.T) {
	ft := &fakeTransactor{}
	var got AuditLog
	hook := func(ctx context.Context, l AuditLog) error {
		got = l
		return nil
	}

	repo := NewRepositoryWithHook(ft, hook)
	entry := NewEntry("User", 42, 7, "update").WithBefore(map[string]any{"a": 1}).WithAfter(map[string]any{"a": 2}).WithDiff([]string{"a"})
	if err := repo.Write(context.Background(), entry); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if !ft.called {
		t.Fatalf("expected transactor WithinTransaction to be called")
	}
	if got.EntityType != "User" || got.EntityID != 42 || got.Action != "update" {
		t.Fatalf("unexpected audit log captured: %+v", got)
	}
}
