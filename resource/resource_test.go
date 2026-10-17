package resource

import (
	"context"
	"errors"
	"testing"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/types"
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

func TestResponse_ToPagination(t *testing.T) {
	t.Parallel()

	got := Response[string]{Data: "row", Meta: map[string]any{"k": 1}}.ToPagination()

	if got.Data != "row" || got.Meta["k"] != 1 {
		t.Fatalf("ToPagination() = %+v", got)
	}
}

type testAuthor struct{ ID int }

func (a testAuthor) GetID() int { return a.ID }

type authoredItem struct {
	ID      uint `gorm:"primaryKey"`
	Created types.Int
	Edited  types.Int
	Ptr     *types.Int
	Null    types.NullInt
}

func TestResource_FindByID_AuthorFieldsOfTheTypesPackage(t *testing.T) {
	t.Parallel()

	conn, cleanup := database.MustPrepareTestDB()
	t.Cleanup(cleanup)

	err := conn.AutoMigrate(&authoredItem{
		ID: 0, Created: types.NewInt(0), Edited: types.NewInt(0), Ptr: nil, Null: types.NewNullInt(0),
	})
	if err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	ptr := types.NewInt(9)

	conn.Create(&authoredItem{
		ID: 1, Created: types.NewInt(7), Edited: types.NewInt(8), Ptr: &ptr, Null: types.NewNullInt(5),
	})

	loader := func(_ *gorm.DB, ids []int) ([]any, error) {
		out := make([]any, 0, len(ids))
		for _, id := range ids {
			out = append(out, testAuthor{ID: id})
		}

		return out, nil
	}

	for _, tcase := range []struct{ name, author, editor string }{
		{"Int", "Created", "Edited"},
		{"*Int", "Ptr", "Ptr"},
		{"NullInt", "Null", "Null"},
	} {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			res := New[authoredItem](conn).WithAuthorLoader(tcase.author, tcase.editor, loader)

			resp, err := res.FindByID(context.Background(), 1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Meta["author"] == nil || resp.Meta["editor"] == nil {
				t.Errorf("author and editor must be loaded, got %v and %v", resp.Meta["author"], resp.Meta["editor"])
			}
		})
	}
}
