package bootstrap

import (
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/auth"
	"gorm.io/gorm"
)

// Module is implemented by every domain package that wants to register
// routes and initialise its own dependencies.
//
// Register is called once at startup for each module, in the order they were
// added in main.go. It receives the shared container and the two root route
// groups -- public (no auth) and protected (JWT required).
//
// Adding a new domain to the application:
//  1. Create internal/domain/orders/module.go implementing Module.
//  2. Add one line to main.go: modules = append(modules, orders.NewModule()).
//  3. Done. No other file changes.
type Module[T auth.Identifiable] interface {
	Register(c *Container[T], public *gin.RouterGroup, protected *gin.RouterGroup)
}

// Migrator is implemented by every domain package that wants to run database migrations.
// Migrate is called once at startup for each module.
type Migrator interface {
	Migrate(db *gorm.DB) error
}
