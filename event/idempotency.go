package event

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// IdempotencyRecord stores idempotency keys for write-level deduplication.
// Minimal schema: unique key, optional stored response and processed timestamp.
type IdempotencyRecord struct {
	ID          uint            `gorm:"primaryKey"`
	Key         string          `gorm:"uniqueIndex:idx_key;size:191"`
	Response    json.RawMessage `gorm:"type:json"`
	CreatedAt   time.Time
	ProcessedAt *time.Time `gorm:"index:idx_processed"`
}

// EnsureIdempotencySchema creates the idempotency table if it doesn't exist.
func EnsureIdempotencySchema(db *gorm.DB) error {
	return db.AutoMigrate(&IdempotencyRecord{})
}

// ReserveKey attempts to reserve an idempotency key using the provided transaction/db.
// Returns (true, nil) when reservation succeeded (new key inserted).
// Returns (false, nil) when the key already exists.
// Returns (false, err) when an unexpected error occurred.
func ReserveKey(ctx context.Context, tx *gorm.DB, key string) (bool, error) {
	if tx == nil {
		return false, gorm.ErrInvalidDB
	}
	if key == "" {
		// empty key => cannot reserve
		return false, nil
	}
	r := IdempotencyRecord{Key: key}
	if err := tx.WithContext(ctx).Create(&r).Error; err != nil {
		if isUniqueConstraintError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ConfirmKey marks the key as processed and optionally stores a response payload.
func ConfirmKey(ctx context.Context, tx *gorm.DB, key string, resp []byte) error {
	if tx == nil {
		return gorm.ErrInvalidDB
	}
	now := time.Now().UTC()
	updates := map[string]interface{}{"processed_at": &now}
	if resp != nil {
		updates["response"] = resp
	}
	return tx.WithContext(ctx).Model(&IdempotencyRecord{}).Where("key = ?", key).Updates(updates).Error
}

// GetResponse fetches stored response for a key if present. Found==false when no record.
func GetResponse(ctx context.Context, db *gorm.DB, key string) (resp []byte, found bool, err error) {
	if db == nil {
		return nil, false, gorm.ErrInvalidDB
	}
	var r IdempotencyRecord
	if err := db.WithContext(ctx).Where("key = ?", key).First(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return r.Response, true, nil
}

// isUniqueConstraintError performs a best-effort detection of unique constraint
// violation errors across common SQL drivers. This is intentionally minimal.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	// SQLite: "unique constraint failed: table.column"
	// MySQL : "error 1062: duplicate entry '...' for key '..."'
	// PgSQL : "duplicate key value violates unique constraint"
	return strings.Contains(s, "unique constraint failed") ||
		strings.Contains(s, "duplicate entry") ||
		strings.Contains(s, "duplicate key value")
}
