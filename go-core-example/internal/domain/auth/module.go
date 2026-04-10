// Package auth provides the authentication module for the go-core-example
// application. It exposes a login endpoint and a DB-backed IdentityResolver.
package auth

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
)

// Module wires the auth domain into the application container.
// It has no business logic — that lives in service.go and handler.go.
type Module struct {
	tokenCfg coreauth.TokenConfig
	svc      Service
}

// NewModule creates an auth module with the given token configuration.
func NewModule(tokenCfg coreauth.TokenConfig) *Module {
	return &Module{tokenCfg: tokenCfg}
}

func (m *Module) Name() string { return "auth" }

// Register migrates the users table, seeds a demo admin, and wires routes.
func (m *Module) Register(c *bootstrap.Container) error {
	db := bootstrap.MustResolve[*database.Registry](c).Primary()

	if err := database.SafeMigrate(db, &User{}); err != nil {
		return fmt.Errorf("auth: migrate: %w", err)
	}

	// Seed a demo admin user if the table is empty.
	// Remove or replace with proper onboarding in production.
	var count int64
	db.Model(&User{}).Count(&count)
	if count == 0 {
		db.Create(&User{
			Username:     "admin",
			PasswordHash: "demo", // replace with bcrypt hash in production
			Policies:     []string{"products:create", "products:update", "products:delete"},
		})
	}

	m.svc = newService(db, m.tokenCfg)
	h := newHandler(m.svc)

	eng, err := bootstrap.Resolve[*gin.Engine](c)
	if err != nil {
		return fmt.Errorf("auth: resolve engine: %w", err)
	}

	eng.Group("/api/v1/auth").POST("/login", h.login)

	return nil
}

// IdentityResolver returns a coreauth.IdentityResolver backed by the DB.
// Pass this to bootstrap.WithJWTAuth. The service is initialised during
// Register(), which always runs before the HTTP server starts.
//
//	authMod := auth.NewModule(tokenCfg)
//	bootstrap.New(cfg).WithJWTAuth(authMod.IdentityResolver).WithModules(authMod, ...)
func (m *Module) IdentityResolver(ctx context.Context, id string) (coreauth.Identifiable, error) {
	return m.svc.ResolveIdentity(ctx, id)
}

// Boot is a no-op — the auth module has no background workers.
func (m *Module) Boot(_ context.Context) error { return nil }

// Shutdown is a no-op — the auth module has no resources to release.
func (m *Module) Shutdown(_ context.Context) error { return nil }
