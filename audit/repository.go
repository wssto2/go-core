// internal/audit/repository.go
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wssto2/go-core/database"
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
	DealerID      int
	BeforeState   any // will be marshalled to JSON
	AfterState    any
	ChangedFields []string
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
		return fmt.Errorf("marshal before state: %w", err)
	}
	after, err := json.Marshal(entry.AfterState)
	if err != nil {
		return fmt.Errorf("marshal after state: %w", err)
	}
	fields, err := json.Marshal(entry.ChangedFields)
	if err != nil {
		return fmt.Errorf("marshal changed fields: %w", err)
	}

	log := AuditLog{
		EntityType:    entry.EntityType,
		EntityID:      entry.EntityID,
		Action:        entry.Action,
		ActorID:       entry.ActorID,
		DealerID:      entry.DealerID,
		BeforeState:   before,
		AfterState:    after,
		ChangedFields: fields,
		CreatedAt:     time.Now(),
	}

	return r.db(ctx).Create(&log).Error
}
