package database

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// migrationLock is a lightweight table used to guard migrations from running
// concurrently. It's intentionally minimal and internal to this package.
type migrationLock struct {
	ID       int        `gorm:"primaryKey"`
	Locked   bool       `gorm:"column:locked"`
	LockedAt *time.Time `gorm:"column:locked_at"`
}

func acquireMigrationLock(db *gorm.DB) (bool, error) {
	// Ensure the lock table exists
	if err := db.AutoMigrate(&migrationLock{}); err != nil {
		return false, fmt.Errorf("migrate lock table: %w", err)
	}

	// Ensure a row with ID=1 exists
	if err := db.FirstOrCreate(&migrationLock{}, migrationLock{ID: 1}).Error; err != nil {
		return false, fmt.Errorf("ensure lock row: %w", err)
	}

	// Try to atomically acquire the lock only if it's not already locked.
	res := db.Model(&migrationLock{}).
		Where("id = ? AND (locked = ? OR locked IS NULL)", 1, false).
		Updates(map[string]interface{}{"locked": true, "locked_at": time.Now()})
	if res.Error != nil {
		return false, fmt.Errorf("acquire migration lock: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		// Lock is held by another process
		return false, nil
	}
	return true, nil
}

func releaseMigrationLock(db *gorm.DB) error {
	res := db.Model(&migrationLock{}).Where("id = ?", 1).
		Updates(map[string]interface{}{"locked": false, "locked_at": nil})
	return res.Error
}

// SafeMigrate runs AutoMigrate with a lightweight migration lock to avoid
// concurrent migrations. It returns an error if a lock cannot be acquired.
func SafeMigrate(db *gorm.DB, models ...interface{}) error {
	acquired, err := acquireMigrationLock(db)
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("migration locked by another process")
	}
	defer func() {
		_ = releaseMigrationLock(db)
	}()

	return db.AutoMigrate(models...)
}
