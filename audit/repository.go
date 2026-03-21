package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/logger"
	"gorm.io/gorm"
)

type Repository interface {
	Write(ctx context.Context, entry Entry) error
}

type Entry struct {
	EntityType    string
	EntityID      int
	Action        string
	ActorID       int
	Metadata      map[string]any
	BeforeState   any // will be marshalled to JSON
	AfterState    any
	ChangedFields []string
}

func NewEntry(entityType string, entityID, actorID int, action string) Entry {
	return Entry{EntityType: entityType, EntityID: entityID, Action: action, ActorID: actorID}
}

func (e Entry) WithBefore(v any) Entry {
	e.BeforeState = v
	return e
}

func (e Entry) WithAfter(v any) Entry {
	e.AfterState = v
	return e
}

func (e Entry) WithDiff(fields []string) Entry {
	e.ChangedFields = fields
	return e
}

type gormRepository struct {
	conn *gorm.DB
}

func NewRepository(conn *gorm.DB) Repository {
	return &gormRepository{conn: conn}
}

func (r *gormRepository) db(ctx context.Context) *gorm.DB {
	if tx, ok := database.TxFromContext(ctx); ok {
		return tx.WithContext(ctx)
	}
	return r.conn.WithContext(ctx)
}

func (r *gormRepository) Write(ctx context.Context, entry Entry) error {
	before, err := json.Marshal(entry.BeforeState)
	if err != nil {
		logger.Log.ErrorContext(ctx, "audit: failed to marshal before state", "error", err, "entity_type", entry.EntityType)
		return err
	}
	after, err := json.Marshal(entry.AfterState)
	if err != nil {
		logger.Log.ErrorContext(ctx, "audit: failed to marshal after state", "error", err, "entity_type", entry.EntityType)
		return err
	}
	fields, err := json.Marshal(entry.ChangedFields)
	if err != nil {
		logger.Log.ErrorContext(ctx, "audit: failed to marshal changed fields", "error", err, "entity_type", entry.EntityType)
		return err
	}

	log := AuditLog{
		EntityType:    entry.EntityType,
		EntityID:      entry.EntityID,
		Action:        entry.Action,
		ActorID:       entry.ActorID,
		Metadata:      entry.Metadata,
		BeforeState:   before,
		AfterState:    after,
		ChangedFields: fields,
		CreatedAt:     time.Now(),
	}

	if err := r.db(ctx).Create(&log).Error; err != nil {
		logger.Log.ErrorContext(ctx, "audit: failed to create log entry", "error", err, "entity_type", entry.EntityType)
		return err
	}

	return nil
}
