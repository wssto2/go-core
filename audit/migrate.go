package audit

import "gorm.io/gorm"

// Migrate creates or updates the audit_logs table to match the current AuditLog schema.
// Call this during application startup before the first audit write.
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&AuditLog{})
}
