package bootstrap

import "github.com/gin-gonic/gin"

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
type Module interface {
	Register(c *Container, public, protected *gin.RouterGroup)
}
