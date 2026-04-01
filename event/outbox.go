package event

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// OutboxEvent represents a stored event awaiting delivery.
type OutboxEvent struct {
	ID        uint   `gorm:"primaryKey"`
	RequestID string `gorm:"index:idx_request_id"`
	Source    string
	Envelope  json.RawMessage `gorm:"type:json"`
	CreatedAt time.Time
	// ProcessedAt is set when the event has been successfully published.
	ProcessedAt *time.Time `gorm:"index:idx_processed"`
}

// EnsureOutboxSchema creates the outbox table if it doesn't exist.
func EnsureOutboxSchema(db *gorm.DB) error {
	return db.AutoMigrate(&OutboxEvent{})
}

// InsertOutboxEvent inserts the provided envelope into the outbox using the
// given transaction. This is designed to be called inside an existing DB
// transaction so application writes and outbox writes commit atomically.
func InsertOutboxEvent(ctx context.Context, tx *gorm.DB, env *Envelope) error {
	if tx == nil {
		return gorm.ErrInvalidDB
	}
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	e := OutboxEvent{
		RequestID: env.RequestID,
		Source:    env.Source,
		Envelope:  b,
	}
	return tx.WithContext(ctx).Create(&e).Error
}

// FetchPending fetches up to limit pending events (not processed) ordered by ID.
func FetchPending(ctx context.Context, db *gorm.DB, limit int) ([]OutboxEvent, error) {
	var out []OutboxEvent
	if err := db.WithContext(ctx).
		Where("processed_at IS NULL").
		Order("id ASC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// MarkProcessed marks the given event id as processed using the provided tx.
func MarkProcessed(ctx context.Context, tx *gorm.DB, id uint) error {
	if tx == nil {
		return gorm.ErrInvalidDB
	}
	now := time.Now()
	return tx.WithContext(ctx).Model(&OutboxEvent{}).Where("id = ?", id).Update("processed_at", &now).Error
}
