package product

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/logger"
	"gorm.io/gorm"
)

// Module wires the product domain into the application.
type Module struct{}

func NewModule() *Module { return &Module{} }

// Register constructs the full product dependency chain, attaches routes,
// and subscribes to all events this domain cares about.
// Everything the product domain needs is contained here.
func (m *Module) Register(c *bootstrap.Container, _, protected *gin.RouterGroup) {
	db := c.DB("local")

	repo := NewRepository(db)
	svc := NewService(repo, c.Transactor("local"), c.AuditRepo(), c.Bus())
	h := newHandler(svc, db)

	h.registerRoutes(protected.Group("/products"))

	// Subscribe to events this domain reacts to.
	// Cross-domain reactions (e.g. send an email on create) live here,
	// not in a central events file that grows with every new domain.
	if err := c.Bus().Subscribe(ProductCreatedEvent{}, onProductCreated); err != nil {
		panic("product: failed to subscribe to ProductCreatedEvent: " + err.Error())
	}
}

func (m *Module) Migrate(db *gorm.DB) error {
	return database.Migrate(db, &Product{}, &audit.AuditLog{})
}

// onProductCreated reacts to a successfully created product.
// In production: send confirmation email, warm cache, notify webhook, etc.
func onProductCreated(ctx context.Context, e any) error {
	ev, ok := e.(ProductCreatedEvent)
	if !ok {
		return fmt.Errorf("onProductCreated: unexpected event type %T", e)
	}
	logger.Log.InfoContext(ctx, "product.created",
		"product_id", ev.ProductID,
		"sku", ev.SKU,
	)
	return nil
}
