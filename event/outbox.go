package event

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// OutboxEvent represents a stored event awaiting delivery.
type OutboxEvent struct {
	ID        uint   `gorm:"primaryKey"`
	RequestID string `gorm:"index:idx_request_id"`
	Source    string `gorm:"size:191"`
	// EventType is the delivery subject: the durable name of an event declared
	// with Define.
	EventType string          `gorm:"size:191"`
	Envelope  json.RawMessage `gorm:"type:json"`
	CreatedAt time.Time
	// ProcessedAt is set when the event has been successfully published.
	ProcessedAt *time.Time `gorm:"index:idx_processed"`
}

// EnsureOutboxSchema creates or migrates the outbox table.
func EnsureOutboxSchema(db *gorm.DB) error {
	return db.AutoMigrate(&OutboxEvent{})
}
