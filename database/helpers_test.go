package database

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
	"time"
)

type docTestModel struct {
	ID             int `gorm:"primaryKey"`
	DocumentNumber int
	Year           int
}

func TestGetNextDocumentNumberAndYear_Quoting(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	if err := db.AutoMigrate(&docTestModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	year := time.Now().Year()
	// Insert a record for this year
	db.Create(&docTestModel{DocumentNumber: 5, Year: year})

	num, gotYear, err := GetNextDocumentNumberAndYear[docTestModel](db, "document_number", "year")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != 6 {
		t.Errorf("expected next document number 6, got %d", num)
	}
	if gotYear != year {
		t.Errorf("expected year %d, got %d", year, gotYear)
	}
}
