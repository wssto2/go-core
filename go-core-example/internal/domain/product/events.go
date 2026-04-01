package product

// ProductCreatedEvent is published after a product is successfully persisted.
// Subscribers (email, cache invalidation, webhooks) react to this event
// without coupling to the product service directly.
type ProductCreatedEvent struct {
	ProductID int
	SKU       string
}
