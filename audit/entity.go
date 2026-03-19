package audit

import (
	"encoding/json"
	"time"
)

type AuditLog struct {
	ID            uint            `gorm:"primaryKey"`
	EntityType    string          `gorm:"not null"`
	EntityID      int             `gorm:"not null"`
	Action        string          `gorm:"not null"`
	ActorID       int             `gorm:"not null"`
	DealerID      int             `gorm:"not null"`
	BeforeState   json.RawMessage `gorm:"type:json"`
	AfterState    json.RawMessage `gorm:"type:json"`
	ChangedFields json.RawMessage `gorm:"type:json"`
	CreatedAt     time.Time       `gorm:"not null"`
}
