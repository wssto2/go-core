package product

import (
	"time"

	"github.com/wssto2/go-core/database/types"
)

// Product is the core GORM entity for this example application.
// It demonstrates go-core's custom database types:
//   - types.NullString  — nullable varchar, marshals empty string as JSON null
//   - types.NullInt     — nullable int FK (created_by / updated_by author pattern)
//   - types.NullDateTime — nullable datetime
//   - types.Float       — decimal(10,2) with proper JSON serialisation
//   - types.Bool        — tinyint(1) unsigned with boolean JSON
type Product struct {
	ID          int              `json:"id"           gorm:"primaryKey;autoIncrement"`
	Name        string           `json:"name"         gorm:"size:150;not null"`
	SKU         string           `json:"sku"          gorm:"size:50;not null;uniqueIndex"`
	Description types.NullString `json:"description"  gorm:"size:1000"`
	Price       types.Float      `json:"price"        gorm:"type:decimal(10,2);not null"`
	Stock       int              `json:"stock"        gorm:"not null;default:0"`
	Active      types.Bool       `json:"active"       gorm:"not null;default:false"`
	CategoryID  types.NullInt    `json:"category_id"  gorm:"type:int unsigned"`

	// Author tracking — mirrors the pattern used across arv-next entities.
	// CreatedBy is always set; UpdatedBy is nullable (nil until first edit).
	CreatedBy int           `json:"created_by"   gorm:"type:int unsigned;not null"`
	UpdatedBy types.NullInt `json:"updated_by"   gorm:"type:int unsigned"`

	CreatedAt time.Time          `json:"created_at"   gorm:"not null"`
	UpdatedAt types.NullDateTime `json:"updated_at"`
	DeletedAt types.NullDateTime `json:"deleted_at"   gorm:"index"`
}
