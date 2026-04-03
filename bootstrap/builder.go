package bootstrap

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/health"
	"github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/logger"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/observability"
	"github.com/wssto2/go-core/observability/metrics"
)

// AppBuilder constructs an App using a fluent chain of calls.
type AppBuilder struct {
	cfg       Config
	container *Container
	engine    *gin.Engine
	modules   []Module
	server    HTTPServer
	errors    []error // accumulate wiring errors, report all at once in build
}

// New creates a new AppBuilder with the given config.
func New(cfg Config) *AppBuilder {
	engine := gin.New()

	return &AppBuilder{
		cfg:       cfg,
		container: NewContainer(),
		engine:    engine,
	}
}

// DefaultInfrastructure wires every core service into the container
// and registers infrastructure HTTP routes. Call this first, before
// WithModules, so modules can resolve these services.
func (b *AppBuilder) DefaultInfrastructure() *AppBuilder {
	b.setupLogger()
	b.setupDatabase()
	b.setupBus()
	b.setupI18n()
	b.setupObservability()
	b.setupHealth()
	b.setupHTTPMiddlewares()

	// Bind the engine so modules can register routes
	Bind(b.container, b.engine)

	return b
}

func (b *AppBuilder) setupLogger() {
	log, err := logger.New(logger.Config{
		AppName:    b.cfg.AppName,
		LogDir:     b.cfg.Log.LogDir,
		Env:        b.cfg.Env,
		Level:      b.cfg.Log.Level,
		MaxSizeMB:  b.cfg.Log.MaxSizeMB,
		MaxBackups: b.cfg.Log.MaxBackups,
		MaxAgeDays: b.cfg.Log.MaxAgeDays,
	})
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("logger: %w", err))
		return
	}
	OverwriteBind(b.container, log)
}

func (b *AppBuilder) setupDatabase() {
	if len(b.cfg.Databases) == 0 {
		return // database is optional
	}

	log := MustResolve[*slog.Logger](b.container)

	reg := database.NewRegistryFromConfigs(log, b.cfg.DatabaseRegistry, b.cfg.Databases)
	OverwriteBind(b.container, reg)

	// Audit repo — only when a primary DB exists
	if reg.PrimaryName() != "" {
		tx := database.NewTransactor(reg.Primary())
		auditRepo := audit.NewRepository(tx)
		Bind(b.container, auditRepo)
	}
}

func (b *AppBuilder) setupBus() {
	// Default to in-memory. Apps call BindNATSBus() after DefaultInfrastructure
	// to swap this out before modules are registered.
	bus := event.NewInMemoryBus()
	Bind[event.Bus](b.container, bus)
}

func (b *AppBuilder) setupI18n() {
	if b.cfg.I18n.I18nDir == "" {
		return // i18n is optional
	}
	t, err := i18n.New(b.cfg.I18n)
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("i18n: %w", err))
		return
	}
	OverwriteBind(b.container, t)
}

func (b *AppBuilder) setupObservability() {
	log := MustResolve[*slog.Logger](b.container)
	tel := observability.New(log)
	OverwriteBind(b.container, tel)

	// Expose /metrics using the app's own registry — not the global default
	b.engine.GET("/metrics", gin.WrapH(
		promhttp.HandlerFor(tel.Registry, promhttp.HandlerOpts{}),
	))
}

func (b *AppBuilder) setupHealth() {
	hr := health.NewHealthRegistry()
	OverwriteBind(b.container, hr)

	// Add DB health checker if a registry is bound
	if reg, err := Resolve[*database.Registry](b.container); err == nil {
		if reg.PrimaryName() != "" {
			hr.Add(health.NewDBHealthChecker(reg.Primary()))
		}
	}

	// Add EventBus health checker if bus is bound
	if bus, err := Resolve[event.Bus](b.container); err == nil {
		hr.Add(health.NewEventBusChecker(bus))
	}

	b.engine.GET("/health", health.LivenessHandler())
	b.engine.GET("/ready", health.ReadinessHandler(hr))
}

func (b *AppBuilder) setupHTTPMiddlewares() {
	log := MustResolve[*slog.Logger](b.container)
	tel := MustResolve[*observability.Telemetry](b.container)
	translator, err := Resolve[*i18n.Translator](b.container)
	if err != nil {
		translator = nil
	}

	b.engine.Use(
		middlewares.RequestID(),
		middlewares.RequestLogger(log),
		metrics.InstrumentHTTP(tel.HTTP),
		middlewares.PanicRecovery(log),
		middlewares.ErrorHandler(log, translator, b.cfg.Env != "production"),
		middlewares.Security(b.cfg.Env != "production"),
		middlewares.Cors(b.cfg.Engine.Cors),
	)
}

// WithModules registers domain modules in order. Each module receives
// the container to resolve infrastructure and bind its own services.
func (b *AppBuilder) WithModules(modules ...Module) *AppBuilder {
	b.modules = append(b.modules, modules...)
	return b
}

// WithNATSBus swaps the default in-memory bus for a NATS-backed bus.
// Must be called after DefaultInfrastructure and before Build.
func (b *AppBuilder) WithNATSBus(client event.NatsClient) *AppBuilder {
	log := MustResolve[*slog.Logger](b.container)
	n := event.NewNATSBus(client, log)
	OverwriteBind[event.Bus](b.container, n) // overwrites the in-memory default
	return b
}

func (b *AppBuilder) WithJWTAuth(resolver auth.IdentityResolver) *AppBuilder {
	if b.cfg.JWT.Secret == "" {
		panic("WithJWTAuth: JWT_SECRET must not be empty")
	}
	if len(b.cfg.JWT.Secret) < 32 {
		panic("WithJWTAuth: JWT_SECRET must be at least 32 characters for HS256 security")
	}
	tokenCfg := auth.TokenConfig{
		SecretKey:     b.cfg.JWT.Secret,
		Issuer:        b.cfg.JWT.Issuer,
		TokenDuration: b.cfg.JWT.Duration,
	}

	authProvider := auth.NewJWTProvider(tokenCfg, resolver)
	Bind[auth.Provider](b.container, authProvider)

	return b
}

func (b *AppBuilder) WithDBTokenAuth(store auth.TokenStore, resolver auth.IdentityResolver) *AppBuilder {
	authProvider := auth.NewDBTokenProvider(store, resolver)
	Bind[auth.Provider](b.container, authProvider)

	return b
}

func (b *AppBuilder) WithHttp() *AppBuilder {
	port := b.cfg.Port
	if port == 0 {
		port = 8080
	}

	if b.cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	if b.cfg.Engine.StaticPath != "" && b.cfg.Engine.StaticURL != "" {
		b.engine.Static(b.cfg.Engine.StaticURL, b.cfg.Engine.StaticPath)
	}

	readHeaderTimeout := time.Duration(b.cfg.ReadHeaderTimeoutSec) * time.Second
	if readHeaderTimeout <= 0 {
		readHeaderTimeout = 10 * time.Second
	}
	readTimeout := time.Duration(b.cfg.ReadTimeoutSec) * time.Second
	if readTimeout <= 0 {
		readTimeout = 30 * time.Second
	}
	writeTimeout := time.Duration(b.cfg.WriteTimeoutSec) * time.Second
	if writeTimeout <= 0 {
		writeTimeout = 30 * time.Second
	}
	idleTimeout := time.Duration(b.cfg.IdleTimeoutSec) * time.Second
	if idleTimeout <= 0 {
		idleTimeout = 120 * time.Second
	}
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           b.engine,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	b.server = &serverWrapper{srv: srv}

	return b
}

// Build validates accumulated errors, registers all modules, and
// returns the runnable *App. Returns an error if any wiring step failed.
func (b *AppBuilder) Build() (*App, error) {
	if len(b.errors) > 0 {
		return nil, fmt.Errorf("bootstrap: wiring errors: %v", b.errors)
	}

	return NewApp(b.cfg, b.container, b.engine, b.server, b.modules), nil
}
