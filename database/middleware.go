package database

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const contextKey = "db"

// UseDatabaseConnection returns a gin middleware that retrieves the named
// connection from the registry and stores it in the request context.
//
// This replaces the original guards.UseDatabaseConnection middleware.
// The registry is injected at startup — no global state needed.
//
// Usage in routes:
//
//	reg := database.NewRegistry(cfg)
//	reg.MustRegister(database.ConnectionConfig{Name: "local", ...})
//
//	router.GET("/customers",
//	    database.UseDatabaseConnection(reg, "local"),
//	    handler,
//	)
func UseDatabaseConnection(reg *Registry, name string) gin.HandlerFunc {
	// Validate at startup that the connection exists, not at request time.
	// If the connection isn't registered this panics immediately when routes
	// are set up — not silently at runtime during the first request.
	if !reg.Has(name) {
		panic("database.UseDatabaseConnection: connection " + name + " not registered in registry")
	}

	return func(ctx *gin.Context) {
		conn, err := reg.Get(name)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "database unavailable",
			})
			return
		}

		ctx.Set(contextKey, conn)
		ctx.Next()
	}
}

// ConnFromContext retrieves the *gorm.DB stored by UseDatabaseConnection.
// Returns false if no connection is in the context.
func ConnFromContext(ctx *gin.Context) (*gorm.DB, bool) {
	raw, exists := ctx.Get(contextKey)
	if !exists {
		return nil, false
	}
	conn, ok := raw.(*gorm.DB)
	return conn, ok
}

// MustConnFromContext retrieves the *gorm.DB from context.
// Panics if not present — use only in handlers protected by UseDatabaseConnection.
func MustConnFromContext(ctx *gin.Context) *gorm.DB {
	conn, ok := ConnFromContext(ctx)
	if !ok {
		panic("database.MustConnFromContext: no database connection in context — is UseDatabaseConnection middleware applied?")
	}
	return conn
}
