package resource

import (
	"context"
	"errors"
	"testing"

	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
)

type testItem struct {
	ID uint `gorm:"primaryKey"`
}

func setupDB(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	db, cleanup := database.MustPrepareTestDB()
	if err := db.AutoMigrate(&testItem{}); err != nil {
		cleanup()
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db, cleanup
}

func TestResource_FindByID_ReturnsRecord(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	db.Create(&testItem{ID: 1})

	resp, err := New[testItem](db).FindByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Data.ID != 1 {
		t.Errorf("expected ID=1, got %d", resp.Data.ID)
	}
}

func TestResource_FindByID_ZeroIDReturnsError(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	_, err := New[testItem](db).FindByID(context.Background(), 0)
	if err == nil {
		t.Fatal("expected error for id=0, got nil")
	}
}

func TestResource_FindByID_NegativeIDReturnsError(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	_, err := New[testItem](db).FindByID(context.Background(), -5)
	if err == nil {
		t.Fatal("expected error for id=-5, got nil")
	}
}

func TestResource_FindByID_MultipleCountsReturnedInMeta(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	db.Create(&testItem{ID: 1})

	resp, err := New[testItem](db).
		WithCount("test_items", "id", "").
		WithCount("test_items", "id", "id > 0").
		FindByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := resp.Meta["test_items_count"]; !ok {
		t.Error("expected test_items_count in meta")
	}
	if _, ok := resp.Meta["test_items_count_2"]; !ok {
		t.Error("expected test_items_count_2 in meta (collision dedup)")
	}
}

func TestResource_FindByID_NotFoundReturnsError(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	_, err := New[testItem](db).FindByID(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error for missing record, got nil")
	}
}

func TestResource_WithCount_EmptyTableNameSetsError(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	_, err := New[testItem](db).
		WithCount("", "id", "").
		FindByID(context.Background(), 1)
	if !errors.Is(err, err) || err == nil {
		t.Fatal("expected error when tableName is empty")
	}
}
