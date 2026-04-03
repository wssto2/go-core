// Package event provides event bus, outbox, and deduplication primitives.
//
// # Idempotency (event/DB level)
//
// DBProcessedStore and the package-level ReserveKey/ConfirmKey/GetResponse
// functions provide persistent, transaction-safe deduplication keyed by an
// arbitrary string. DBProcessedStore implements ProcessedStore, the same
// interface as InMemoryProcessedStore, so callers can swap implementations
// without changing business logic.
//
// This is a distinct concern from the HTTP-level idempotency middleware in
// the middlewares package. That middleware buffers full HTTP responses
// (status, headers, body) and replays them for duplicate requests identified
// by the Idempotency-Key header.
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

// DBProcessedStore is a database-backed implementation of ProcessedStore.
// It uses IdempotencyRecord for persistent, transaction-safe deduplication.
// Use NewDBProcessedStore to construct one; the underlying *gorm.DB can be a
// transaction if callers need transactional reservation.
type DBProcessedStore struct {
	db *gorm.DB
}

// NewDBProcessedStore creates a DBProcessedStore backed by the given db.
func NewDBProcessedStore(db *gorm.DB) *DBProcessedStore {
	return &DBProcessedStore{db: db}
}

// Reserve attempts to reserve the given id. Returns true if the reservation
// succeeded (id was not previously reserved or processed).
func (s *DBProcessedStore) Reserve(ctx context.Context, id string) (bool, error) {
	return ReserveKey(ctx, s.db, id)
}

// Confirm marks the id as successfully processed.
func (s *DBProcessedStore) Confirm(ctx context.Context, id string) error {
	return ConfirmKey(ctx, s.db, id, nil)
}

// Release removes the reservation so the id can be retried later.
func (s *DBProcessedStore) Release(ctx context.Context, id string) error {
	if s.db == nil {
		return gorm.ErrInvalidDB
	}
	if id == "" {
		return nil
	}
	return s.db.WithContext(ctx).Where("key = ?", id).Delete(&IdempotencyRecord{}).Error
}

// GetResponse fetches the stored response payload for a key, if present.
// This is an optional capability beyond the base ProcessedStore interface.
func (s *DBProcessedStore) GetResponse(ctx context.Context, id string) ([]byte, bool, error) {
	return GetResponse(ctx, s.db, id)
}

// PurgeStaleReservations deletes idempotency records that were reserved but never
// confirmed within olderThan. These accumulate when a handler crashes after Reserve
// but before Confirm, permanently blocking re-processing of that key.
// Records where created_at < now-olderThan and processed_at IS NULL are removed.
func (s *DBProcessedStore) PurgeStaleReservations(ctx context.Context, olderThan time.Duration) error {
	if s.db == nil {
		return gorm.ErrInvalidDB
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	return s.db.WithContext(ctx).
		Where("processed_at IS NULL AND created_at < ?", cutoff).
		Delete(&IdempotencyRecord{}).Error
}

// compile-time assertion that DBProcessedStore satisfies ProcessedStore.
var _ ProcessedStore = (*DBProcessedStore)(nil)
