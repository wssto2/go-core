package product

import (
	"time"

	"github.com/wssto2/go-core/database/types"
)

// ImageStatus values for the Product.ImageStatus field.
const (
	ImageStatusPending    = "pending"
	ImageStatusProcessing = "processing"
	ImageStatusDone       = "done"
	ImageStatusFailed     = "failed"
)

// Product is the core GORM entity for this example application.
type Product struct {
	ID          int              `json:"id"           gorm:"primaryKey;autoIncrement"`
	Name        string           `json:"name"         gorm:"size:150;not null"`
	SKU         string           `json:"sku"          gorm:"size:50;not null;uniqueIndex"`
	Description types.NullString `json:"description"  gorm:"size:1000"`
	Price       types.Float      `json:"price"        gorm:"type:decimal(10,2);not null"`
	Stock       int              `json:"stock"        gorm:"not null;default:0"`
	Active      types.Bool       `json:"active"       gorm:"not null;default:false"`
	CategoryID  types.NullInt    `json:"category_id"  gorm:"type:int unsigned"`

	// Image processing — original is saved synchronously; variants are
	// generated in the background by imageWorker.
	// ImageStatus: "" | "pending" | "processing" | "done" | "failed"
	ImageURL      types.NullString `json:"image_url"       gorm:"size:500"`
	ThumbnailURL  types.NullString `json:"thumbnail_url"   gorm:"size:500"`
	ImageStatus   types.NullString `json:"image_status"    gorm:"size:20"`

	// Author tracking — mirrors the pattern used across arv-next entities.
	CreatedBy int           `json:"created_by"   gorm:"type:int unsigned;not null"`
	UpdatedBy types.NullInt `json:"updated_by"   gorm:"type:int unsigned"`

	CreatedAt time.Time          `json:"created_at"   gorm:"not null"`
	UpdatedAt types.NullDateTime `json:"updated_at"`
	DeletedAt types.NullDateTime `json:"deleted_at"   gorm:"index"`
}
