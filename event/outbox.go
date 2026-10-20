package event

import (
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/wssto2/go-core/database"
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
	UseOutbox(db)

	return db.AutoMigrate(&OutboxEvent{})
}

// outboxes are the pools (*sql.DB) of the connections the event queue lives on.
var outboxes sync.Map //nolint:gochecknoglobals // a set keyed by pool: apps and parallel tests each add their own

// UseOutbox marks db's connection as one the outbox lives on, so Publish joins a transaction on it and refuses one
// on another database. EnsureOutboxSchema, Migrate, NewQueue and gocore's Events call it; it is idempotent.
func UseOutbox(db *gorm.DB) {
	if pool := database.Pool(db); pool != nil {
		outboxes.Store(pool, struct{}{})
	}
}

func isOutbox(pool *sql.DB) bool {
	_, ok := outboxes.Load(pool)

	return ok
}
