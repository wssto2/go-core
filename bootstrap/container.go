package bootstrap

import (
	"log/slog"

	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/cache"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/i18n"
	"gorm.io/gorm"
)

// Container holds every piece of shared infrastructure that modules need.
// It is built once at startup and passed read-only to every module.
//
// T is the application's concrete user type (e.g. auth.User).
// It must satisfy auth.Identifiable.
//
// No HTTP framework types appear here. Container is framework-agnostic.
type Container[T auth.Identifiable] struct {
	registry     *database.Registry
	log          *slog.Logger
	i18n         *i18n.Translator
	bus          event.Bus
	cache        cache.Cache
	authProvider auth.AuthProvider[T]
	auditRepo    audit.Repository
}

// NewContainer builds and returns a fully initialised Container.
func NewContainer[T auth.Identifiable](
	registry *database.Registry,
	log *slog.Logger,
	i18n *i18n.Translator,
	bus event.Bus,
	cache cache.Cache,
) *Container[T] {

	return &Container[T]{
		registry:  registry,
		log:       log,
		i18n:      i18n,
		bus:       bus,
		cache:     cache,
		auditRepo: audit.NewRepository(registry.Primary()),
	}
}

// DB returns the named *gorm.DB from the registry.
// Panics at startup (during module construction) if the name is not registered,
// which is the correct behaviour -- a misconfigured connection is unrecoverable.
func (c *Container[T]) DB(name string) *gorm.DB {
	return c.registry.MustGet(name)
}

// PrimaryDB returns the primary *gorm.DB connection.
func (c *Container[T]) PrimaryDB() *gorm.DB {
	return c.DB(c.registry.PrimaryName())
}

// Transactor returns a new Transactor scoped to the named connection.
// Each module that needs transactions calls this with its own connection name.
func (c *Container[T]) Transactor(dbName string) database.Transactor {
	return database.NewTransactor(c.DB(dbName))
}

func (c *Container[T]) PrimaryTransactor() database.Transactor {
	return database.NewTransactor(c.PrimaryDB())
}

// Registry exposes the raw registry for the shutdown closer in main.go.
func (c *Container[T]) Registry() *database.Registry {
	return c.registry
}

// Log returns the application-wide logger.
func (c *Container[T]) Log() *slog.Logger {
	return c.log
}

// I18n returns the application-wide i18n translator.
func (c *Container[T]) I18n() *i18n.Translator {
	return c.i18n
}

// Bus returns the application-wide event bus.
func (c *Container[T]) Bus() event.Bus {
	return c.bus
}

// Cache returns the application-wide cache.
func (c *Container[T]) Cache() cache.Cache {
	return c.cache
}

// AuthProvider returns the application-wide auth provider.
func (c *Container[T]) AuthProvider() auth.AuthProvider[T] {
	return c.authProvider
}

// AuditRepo returns the application-wide audit repository.
func (c *Container[T]) AuditRepo() audit.Repository {
	return c.auditRepo
}
