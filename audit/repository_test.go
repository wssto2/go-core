//nolint:exhaustruct,exhaustruct_v5,lll,nlreturn,noinlineerr,paralleltest,varnamelen,wsl_v5
package audit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRepositoryWriteUsesTransactionAndHook(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "a.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	var got AuditLog
	hook := func(ctx context.Context, l AuditLog) error {
		if _, ok := database.TxFor(ctx, db); !ok {
			t.Error("the hook runs inside a transaction on the repository's connection")
		}
		got = l
		return nil
	}

	repo := NewRepositoryWithHook(db, hook)
	entry := NewEntry("User", 42, 7, "update").WithBefore(map[string]any{"a": 1}).WithAfter(map[string]any{"a": 2}).WithDiff([]string{"a"})
	if err := repo.Write(context.Background(), entry); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got.EntityType != "User" || got.EntityID != 42 || got.Action != "update" {
		t.Fatalf("unexpected audit log captured: %+v", got)
	}
}

func TestRepositoryWriteIgnoresATransactionOnAnotherConnection(t *testing.T) {
	dir := t.TempDir()
	open := func(name string) *gorm.DB {
		db, err := gorm.Open(sqlite.Open(filepath.Join(dir, name)), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	own, other := open("own.db"), open("other.db")
	if err := Migrate(own); err != nil {
		t.Fatal(err)
	}

	foreign, _ := dbtest.TxContext(t, other)
	if err := NewRepository(own).Write(foreign, NewEntry("User", 1, 2, "create")); err != nil {
		t.Fatalf("Write with a foreign transaction in ctx: %v", err)
	}

	var n int64
	if err := own.Model(&AuditLog{}).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("the row is committed on the repository's own connection: n=%d err=%v", n, err)
	}
}
