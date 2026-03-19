package database

import (
	"gorm.io/gorm"
)

// Migrate runs auto-migration for the given models.
func Migrate(db *gorm.DB, models ...interface{}) error {
	// Disable foreign key constraints during migration to avoid issues with circular dependencies?
	// Usually safer to keep them enabled, but GORM sometimes struggles.
	// We'll trust GORM's default behavior for now.
	
	// Option: db.Set("gorm:table_options", "ENGINE=InnoDB")

	return db.AutoMigrate(models...)
}
