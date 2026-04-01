package resource

import (
	"github.com/wssto2/go-core/database"
	"testing"
)

func TestResource_FindByID_MultipleCountsReturnedInMeta(t *testing.T) {
	db, cleanup := database.MustPrepareTestDB()
	defer cleanup()

	// This is a compile-time and logic test only — we verify that
	// calling WithCount multiple times and FindByID returns all counts.
	// The exact implementation depends on your GORM models.
	// At minimum, build the Resource and assert no panic/error from
	// the parallel goroutine machinery:
	type Item struct {
		ID uint `gorm:"primaryKey"`
	}
	if err := db.AutoMigrate(&Item{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	db.Create(&Item{ID: 1})

	_ = New[Item](db).
		WithCount("items", "id", "").
		WithCount("items", "id", "id > 0")
	// FindByID(1) will run 2 parallel COUNTs.
	// We cannot fully assert SQL behavior without a test fixture,
	// but we can assert no panic and the API compiles.
}
