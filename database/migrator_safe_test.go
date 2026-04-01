package database

import (
	"testing"
)

func TestSafeMigrateCreatesTable(t *testing.T) {
	conn, err := openSQLiteMemory()
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := conn.DB()
	defer sqlDB.Close()

	type Person struct {
		ID   uint
		Name string
	}

	if err := SafeMigrate(conn, &Person{}); err != nil {
		t.Fatalf("SafeMigrate failed: %v", err)
	}

	has := conn.Migrator().HasTable(&Person{})
	if !has {
		t.Fatalf("expected table for Person to exist")
	}
}

func TestSafeMigrateLock(t *testing.T) {
	conn, err := openSQLiteMemory()
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := conn.DB()
	defer sqlDB.Close()

	type Person struct {
		ID   uint
		Name string
	}

	// Create the lock table and insert a locked row to simulate another
	// process holding the migration lock.
	if err := conn.AutoMigrate(&migrationLock{}); err != nil {
		t.Fatalf("AutoMigrate lock: %v", err)
	}
	if err := conn.Create(&migrationLock{ID: 1, Locked: true}).Error; err != nil {
		t.Fatalf("create lock: %v", err)
	}

	err = SafeMigrate(conn, &Person{})
	if err == nil {
		t.Fatalf("expected SafeMigrate to fail due to lock, but it succeeded")
	}

	has := conn.Migrator().HasTable(&Person{})
	if has {
		t.Fatalf("expected Person table not to be created when lock is held")
	}
}
