package product

import "github.com/wssto2/go-core/event"

// The product events. Declared once as values: the name is what is stored with every queued row.
var (
	Created       = event.Define[ProductCreatedEvent]("products.created")
	ImageUploaded = event.Define[ProductImageUploadedEvent]("products.image-uploaded")
)

// ProductCreatedEvent is queued after a product is successfully persisted.
type ProductCreatedEvent struct {
	ProductID int    `json:"product_id"`
	SKU       string `json:"sku"`
}

// ProductImageUploadedEvent is queued after a product image is saved to
// storage. The image consumer generates thumbnails and resized variants in the
// background.
type ProductImageUploadedEvent struct {
	ProductID   int    `json:"product_id"`
	OriginalKey string `json:"original_key"` // storage key of the original file
}
