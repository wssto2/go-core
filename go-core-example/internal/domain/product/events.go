package product

// ProductCreatedEvent is published after a product is successfully persisted.
type ProductCreatedEvent struct {
	ProductID int    `json:"product_id"`
	SKU       string `json:"sku"`
}

// ProductImageUploadedEvent is published (and persisted in the outbox) after a
// product image is saved to storage. The imageWorker subscribes to this event
// and generates thumbnails and resized variants in the background.
type ProductImageUploadedEvent struct {
	ProductID   int    `json:"product_id"`
	OriginalKey string `json:"original_key"` // storage key of the original file
}
