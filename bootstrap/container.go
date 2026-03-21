package bootstrap

import (
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/cache"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"gorm.io/gorm"
)

// Container holds every piece of shared infrastructure that modules need.
// It is built once in main.go and passed to every module's Register method.
// Modules read from it; they never mutate it after construction.
//
// When the application needs a second database connection, add it here.
// No module signature or main.go wiring changes -- modules simply call
// c.DB("shared") instead of c.DB("local").
type Container struct {
	registry      *database.Registry
	primaryDBName string
	cache         cache.Cache
	bus           event.Bus
	auditRepo     audit.Repository
	tokenConfig   auth.TokenConfig
	resolver      auth.Resolver
}

// NewContainer builds and returns a fully initialised Container.
func NewContainer(
	registry *database.Registry,
	primaryDB string,
	tokenConfig auth.TokenConfig,
	resolver auth.Resolver,
) *Container {
	// The primary DB is the source of truth for the audit log and transactor.
	// Modules that need a different connection call c.DB("shared") or c.DB("etx_hr").
	primary := registry.MustGet(primaryDB)

	return &Container{
		registry:      registry,
		primaryDBName: primaryDB,
		cache:         cache.NewInMemoryCache(),
		bus:           event.NewInMemoryBus(),
		auditRepo:     audit.NewRepository(primary),
		tokenConfig:   tokenConfig,
		resolver:      resolver,
	}
}

// DB returns the named *gorm.DB from the registry.
// Panics at startup (during module construction) if the name is not registered,
// which is the correct behaviour -- a misconfigured connection is unrecoverable.
func (c *Container) DB(name string) *gorm.DB {
	return c.registry.MustGet(name)
}

func (c *Container) PrimaryDB() *gorm.DB {
	return c.DB(c.primaryDBName)
}

// Transactor returns a new Transactor scoped to the named connection.
// Each module that needs transactions calls this with its own connection name.
func (c *Container) Transactor(dbName string) database.Transactor {
	return database.NewTransactor(c.DB(dbName))
}

// Registry exposes the raw registry for the shutdown closer in main.go.
func (c *Container) Registry() *database.Registry {
	return c.registry
}

func (c *Container) Cache() cache.Cache {
	return c.cache
}

func (c *Container) Bus() event.Bus {
	return c.bus
}

func (c *Container) AuditRepo() audit.Repository {
	return c.auditRepo
}

func (c *Container) TokenConfig() auth.TokenConfig {
	return c.tokenConfig
}

func (c *Container) Resolver() auth.Resolver {
	return c.resolver
}
