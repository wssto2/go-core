package bootstrap

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/cache"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/logger"
)

type AppBuilder[T auth.Identifiable] struct {
	cfg       Config
	container *Container[T]
	server    *gin.Engine
	modules   []Module[T]
}

// New creates a new AppBuilder with the given config
func New[T auth.Identifiable](cfg Config) *AppBuilder[T] {
	return &AppBuilder[T]{cfg: cfg, container: &Container[T]{}}
}

// NewAnonymous creates a new AppBuilder with anonymous authentication
func NewAnonymous(cfg Config) *AppBuilder[auth.Anonymous] {
	return &AppBuilder[auth.Anonymous]{cfg: cfg}
}

// DefaultInfrastructure initialises Logger, Database registry, I18n,
// InMemoryCache, and InMemoryBus from the values in cfg.
//
// Always call this first, before any With* or Register calls.
func (b *AppBuilder[T]) DefaultInfrastructure() *AppBuilder[T] {
	log := logger.MustNew(b.cfg.Log)
	reg := database.NewRegistryFromConfigs(b.cfg.DatabaseRegistry, b.cfg.Databases)
	trans := i18n.MustNew(b.cfg.I18n)

	b.container = &Container[T]{
		registry:  reg,
		log:       log,
		i18n:      trans,
		bus:       event.NewInMemoryBus(),
		cache:     cache.NewInMemoryCache(),
		auditRepo: audit.NewRepository(reg.Primary()),
	}

	return b
}

func (b *AppBuilder[T]) WithServer(server *gin.Engine) *AppBuilder[T] {
	b.server = server
	return b
}

// WithContainer sets the container for the app
func (b *AppBuilder[T]) WithContainer(container *Container[T]) *AppBuilder[T] {
	b.container = container
	return b
}

// WithDatabase initializes the DB and adds the shutdown hook
func (b *AppBuilder[T]) WithDatabase(cfg []database.ConnectionConfig) *AppBuilder[T] {
	reg := database.NewRegistryFromConfigs(database.RegistryConfig{
		LogLevel: b.cfg.Env,
	}, cfg)

	b.container.registry = reg
	return b
}

// WithLogger initializes the core logger
func (b *AppBuilder[T]) WithLogger(cfg logger.Config) *AppBuilder[T] {
	b.container.log = logger.MustNew(cfg)
	return b
}

// WithI18n initializes the i18n translator
func (b *AppBuilder[T]) WithI18n(trans *i18n.Translator) *AppBuilder[T] {
	b.container.i18n = trans
	return b
}

// WithBus initializes the event bus
func (b *AppBuilder[T]) WithBus(bus event.Bus) *AppBuilder[T] {
	b.container.bus = bus
	return b
}

// WithCache initializes the cache
func (b *AppBuilder[T]) WithCache(cache cache.Cache) *AppBuilder[T] {
	b.container.cache = cache
	return b
}

// WithAuth initializes the auth provider
func (b *AppBuilder[T]) WithAuth(provider auth.AuthProvider[T]) *AppBuilder[T] {
	b.container.authProvider = provider
	return b
}

// WithJWTAuth configures JWT authentication and stores the provider on the
// container so modules can issue/verify tokens via c.AuthProvider().
//
// Auth middleware is applied by the HTTPAdapter, not here.
// Pass the resolver to both WithJWTAuth (for the container provider) and
// to the adapter's WithJWTAuth (for the middleware). See the example main.go.
func (b *AppBuilder[T]) WithJWTAuth(resolver auth.IdentityResolver[T]) *AppBuilder[T] {
	tokenCfg := auth.TokenConfig{
		SecretKey:     b.cfg.JWT.Secret,
		Issuer:        b.cfg.JWT.Issuer,
		TokenDuration: b.cfg.JWT.Duration,
	}
	b.container.authProvider = auth.NewJWTProvider(tokenCfg, resolver)
	return b
}

// WithAuditRepo initializes the audit repository
func (b *AppBuilder[T]) WithAuditRepo(repo audit.Repository) *AppBuilder[T] {
	b.container.auditRepo = repo
	return b
}

// WithModules registers modules into the app
func (b *AppBuilder[T]) WithModules(modules ...Module[T]) *AppBuilder[T] {
	b.modules = append(b.modules, modules...)
	return b
}

// Build validates state, runs migrations, wires modules, and returns a *App.
func (b *AppBuilder[T]) Build() (*App[T], error) {
	if b.container == nil {
		return nil, fmt.Errorf("bootstrap: container is nil — call DefaultInfrastructure() or WithContainer() first")
	}
	if b.server == nil {
		return nil, fmt.Errorf("bootstrap: server is nil — call WithServer(adapter) first")
	}

	app := NewApp(b.cfg, b.container, b.server)

	if err := app.runMigrations(b.container, b.modules); err != nil {
		return nil, fmt.Errorf("bootstrap: migration failed: %w", err)
	}

	app.registerModules(b.container, b.server, b.modules)

	app.AddCloser(func(ctx context.Context) error {
		return b.container.registry.CloseAll()
	})

	return app, nil
}
