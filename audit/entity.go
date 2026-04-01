package audit

import (
	"encoding/json"
	"time"
)

// AuditLog represents a persisted audit entry. Tags are intentionally generic
// (no GORM-specific tags) so runtime packages do not import GORM directly.
// Note: Metadata is stored as raw JSON bytes to ensure GORM AutoMigrate can
// create the correct column type across dialects.
type AuditLog struct {
	ID            uint            `json:"id"`
	EntityType    string          `json:"entity_type"`
	EntityID      int             `json:"entity_id"`
	Action        string          `json:"action"`
	ActorID       int             `json:"actor_id"`
	Metadata      json.RawMessage `json:"metadata"`
	BeforeState   json.RawMessage `json:"before_state"`
	AfterState    json.RawMessage `json:"after_state"`
	ChangedFields json.RawMessage `json:"changed_fields"`
	CreatedAt     time.Time       `json:"created_at"`
}
