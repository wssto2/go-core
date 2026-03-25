package product

import (
	"context"
	"fmt"
	"go-core-example/internal/domain/auth"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
)

// Module wires the product domain into the application.
type Module struct{}

func NewModule() *Module { return &Module{} }

// Register constructs the full product dependency chain, attaches routes,
// and subscribes to all events this domain cares about.
// Everything the product domain needs is contained here.
func (m *Module) Register(c *bootstrap.Container[auth.User], _, protected *gin.RouterGroup) {
	repo := NewRepository(c.PrimaryDB())
	svc := NewService(repo, c.PrimaryTransactor(), c.AuditRepo(), c.Bus(), c.Log())
	h := newHandler(svc)

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
	_, ok := e.(ProductCreatedEvent)
	if !ok {
		return fmt.Errorf("onProductCreated: unexpected event type %T", e)
	}

	return nil
}
