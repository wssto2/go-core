package database

import (
	"testing"
)

// testEntity is a tiny model used only for testing migrations and CRUD.
type testEntity struct {
	ID   int `gorm:"primaryKey"`
	Name string
}

func TestPrepareTestDB_MigratesAndOperates(t *testing.T) {
	db, cleanup := MustPrepareTestDB(&testEntity{})
	defer cleanup()

	// Insert a row
	if err := db.Create(&testEntity{Name: "alice"}).Error; err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	var got testEntity
	if err := db.First(&got, "name = ?", "alice").Error; err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if got.Name != "alice" {
		t.Fatalf("unexpected name %s", got.Name)
	}
}

func TestNewTestRegistryCleanupSimulate(t *testing.T) {
	reg, cleanup := NewTestRegistry("local")
	// ensure we clean up at the end in case of failure
	defer func() {
		_ = cleanup()
	}()

	if !reg.Has("local") {
		t.Fatal("expected registry to have 'local' connection")
	}

	restore := SimulateConnectionLoss(reg, "local")
	if reg.Has("local") {
		t.Fatal("expected connection removed after SimulateConnectionLoss")
	}

	restore()
	if !reg.Has("local") {
		t.Fatal("expected connection restored after restore()")
	}

	if err := cleanup(); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if reg.Has("local") {
		t.Fatal("expected connection removed after cleanup")
	}
}
