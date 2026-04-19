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
	"github.com/wssto2/go-core/frontend"
	"github.com/wssto2/go-core/health"
	"github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/logger"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/observability"
	"github.com/wssto2/go-core/observability/metrics"
	"github.com/wssto2/go-core/ratelimit"
	"github.com/wssto2/go-core/worker"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// AppBuilder constructs an App using a fluent chain of calls.
type AppBuilder struct {
	cfg                 Config
	container           *Container
	engine              *gin.Engine
	modules             []Module
	server              HTTPServer
	errors              []error // accumulate wiring errors, report all at once in build
	perUserLimiter      ratelimit.Limiter
	sharedGlobalLimiter ratelimit.Limiter
	spaConfig           *frontend.SPAConfig
}

// New creates a new AppBuilder with the given config.
func New(cfg Config) *AppBuilder {
	engine := gin.New()
	builder := &AppBuilder{
		cfg:       cfg,
		container: NewContainer(),
		engine:    engine,
	}
	proxies := cfg.HTTP.TrustedProxies
	if len(proxies) == 0 {
		proxies = nil
	}
	if err := engine.SetTrustedProxies(proxies); err != nil {
		builder.errors = append(builder.errors, fmt.Errorf("trusted proxies: %w", err))
	}

	return builder
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
		AppName:    b.cfg.App.Name,
		LogDir:     b.cfg.Log.Dir,
		Env:        b.cfg.App.Env,
		Level:      logger.LogLevel(b.cfg.Log.Level),
		MaxSizeMB:  b.cfg.Log.MaxSize,
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
	if len(b.cfg.Database.Connections) == 0 {
		return // database is optional
	}

	log := MustResolve[*slog.Logger](b.container)

	regCfg := database.RegistryConfig{
		LogLevel:           b.cfg.Database.LogLevel,
		SlowQueryThreshold: b.cfg.Database.SlowQueryThreshold,
	}

	connections := make([]database.ConnectionConfig, 0, len(b.cfg.Database.Connections))
	for _, conn := range b.cfg.Database.Connections {
		connections = append(connections, database.ConnectionConfig{
			Name:            conn.Name,
			Driver:          conn.Driver,
			Host:            conn.Host,
			Port:            conn.Port,
			Database:        conn.Database,
			Username:        conn.Username,
			Password:        conn.Password,
			MaxIdleConns:    conn.MaxIdleConns,
			MaxOpenConns:    conn.MaxOpenConns,
			ConnMaxLifetime: conn.ConnMaxLifetime,
			Debug:           conn.Debug,
		})
	}

	reg := database.NewRegistryFromConfigs(log, regCfg, connections)
	OverwriteBind(b.container, reg)

	// Audit repo — only when a primary DB exists
	if reg.PrimaryName() != "" {
		tx := database.NewTransactor(reg.Primary())
		auditRepo := audit.NewRepository(tx)
		Bind(b.container, auditRepo)
	}
}

func (b *AppBuilder) setupBus() {
	bus := event.NewInMemoryBus()
	Bind[event.Bus](b.container, bus)
}

func (b *AppBuilder) setupI18n() {
	if b.cfg.I18n.Dir == "" {
		return // i18n is optional
	}
	t, err := i18n.New(i18n.Config{
		FallbackLang: b.cfg.I18n.DefaultLocale,
		I18nDir:      b.cfg.I18n.Dir,
	})
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("i18n: %w", err))
		return
	}
	OverwriteBind(b.container, t)
}

func (b *AppBuilder) setupObservability() {
	log := MustResolve[*slog.Logger](b.container)
	tel := observability.New(log, b.cfg.App.Name)
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
		otelgin.Middleware(b.cfg.App.Name),
		middlewares.RequestID(),
		middlewares.RequestLogger(log),
		metrics.InstrumentHTTP(tel.HTTP),
		middlewares.PanicRecovery(log),
		middlewares.ErrorHandler(log, translator, b.cfg.App.Env != "production"),
		middlewares.Security(b.cfg.App.Env != "production"),
		middlewares.Cors(middlewares.CorsConfig{
			AllowOrigins: b.cfg.CORS.AllowedOrigins,
			AllowMethods: b.cfg.CORS.AllowedMethods,
			AllowHeaders: b.cfg.CORS.AllowedHeaders,
		}),
	)

}

// WithRateLimit attaches a global per-IP rate limiter to every route.
// It is applied before authentication, making it effective against bots and
// brute-force attacks that target unauthenticated endpoints (e.g. /auth/login).
//
// The limiter keys by authenticated user ID when a user is present in context,
// WithRateLimit attaches a per-user/IP rate limiter to every route.
// Each authenticated user (or anonymous IP) gets its own independent request
// bucket, making it effective against individual abusers and brute-force
// attacks while leaving capacity for other users unaffected.
//
// Keys: authenticated user ID, falling back to client IP for anonymous
// requests. For a server-wide cap shared across ALL users combine this with
// WithGlobalRateLimit.
//
//	bootstrap.New(cfg).
//	    DefaultInfrastructure().
//	    WithGlobalRateLimit(ratelimit.NewInMemoryLimiter(5000, time.Minute)).
//	    WithRateLimit(ratelimit.NewInMemoryLimiter(300, time.Minute)).
//	    WithJWTAuth(resolver).
//	    ...
func (b *AppBuilder) WithRateLimit(l ratelimit.Limiter) *AppBuilder {
	b.perUserLimiter = l
	return b
}

// WithGlobalRateLimit attaches a single shared rate limiter that counts ALL
// requests regardless of user identity. The entire server shares one bucket,
// so once the limit is exhausted every caller receives 429 until the window
// resets — including legitimate users.
//
// WARNING: this is NOT a DDoS defence and should not be used as one.
// Volumetric attacks must be handled at the infrastructure layer (CDN, WAF,
// cloud load balancer). Any limit low enough to stop a real attack is also
// low enough to block real users during normal traffic spikes (product launch,
// marketing campaign, etc.).
//
// Valid niche uses:
//   - Cost control on an API that charges per call (e.g. AI inference)
//   - Protecting a single, known-capacity backend dependency
//   - Development / staging environments where you want a hard cap
//
// For production abuse prevention prefer WithRateLimit (per-user/IP) combined
// with per-endpoint limits inside each module.
func (b *AppBuilder) WithGlobalRateLimit(l ratelimit.Limiter) *AppBuilder {
	b.sharedGlobalLimiter = l
	return b
}

func (b *AppBuilder) WithModules(modules ...Module) *AppBuilder {
	b.modules = append(b.modules, modules...)
	return b
}

// WithSPA enables the convention-based SPA setup for applications that keep
// their frontend files under ./frontend. The provided state builder is optional
// and is injected into the rendered template as appState.
func (b *AppBuilder) WithSPA(stateBuilder frontend.AppStateBuilder) *AppBuilder {
	cfg := frontend.SPAConfig{
		TemplatesPath: b.cfg.Frontend.TemplatesPath,
		TemplateName:  b.cfg.Frontend.TemplateName,
		APIPrefix:     b.cfg.Frontend.APIPrefix,
		StateBuilder:  stateBuilder,
		DevMode:       b.cfg.App.Env == "development",
		Vite: frontend.ViteConfig{
			Entry:           b.cfg.Frontend.EntryScript,
			ManifestPath:    b.cfg.Frontend.ManifestPath,
			AssetsURLPrefix: b.cfg.Frontend.AssetsURLPrefix,
		},
	}

	if cfg.TemplatesPath == "" {
		cfg.TemplatesPath = "frontend/templates/*.html"
	}

	if cfg.APIPrefix == "" {
		cfg.APIPrefix = "/api"
	}

	if cfg.Vite.Entry == "" {
		cfg.Vite.Entry = "src/main.ts"
	}

	if cfg.Vite.ManifestPath == "" {
		cfg.Vite.ManifestPath = "./frontend/dist/.vite/manifest.json"
	}

	if cfg.Vite.AssetsURLPrefix == "" {
		cfg.Vite.AssetsURLPrefix = "/frontend/dist"
	}

	b.spaConfig = &cfg

	return b
}

// WithSPAConfig enables SPA support with an explicit frontend configuration.
func (b *AppBuilder) WithSPAConfig(cfg frontend.SPAConfig) *AppBuilder {
	if cfg.APIPrefix == "" {
		cfg.APIPrefix = b.cfg.Frontend.APIPrefix
	}

	if cfg.APIPrefix == "" {
		cfg.APIPrefix = "/api"
	}

	if !cfg.DevMode {
		cfg.DevMode = b.cfg.App.Env == "development"
	}

	b.spaConfig = &cfg

	return b
}

// WithTrustedProxies configures the proxies Gin will trust for client-IP and
// forwarded-proto resolution. Passing no values disables proxy trust.
func (b *AppBuilder) WithTrustedProxies(proxies ...string) *AppBuilder {
	if len(proxies) == 0 {
		proxies = nil
	}

	b.cfg.HTTP.TrustedProxies = append([]string(nil), proxies...)
	if err := b.engine.SetTrustedProxies(proxies); err != nil {
		b.errors = append(b.errors, fmt.Errorf("trusted proxies: %w", err))
	}

	return b
}

// WithOutboxWorker adds a background worker that continuously polls the outbox
// table and forwards events via publish. Use this in standalone worker binaries
// that don't need HTTP.
//
//	bootstrap.New(cfg).
//	    DefaultInfrastructure().
//	    WithOutboxWorker(product.NewWebhookPublisher(url, token)).
//	    Build()
func (b *AppBuilder) WithOutboxWorker(publish event.PublishFunc, opts ...event.WorkerOption) *AppBuilder {
	b.modules = append(b.modules, newOutboxModule(publish, opts))
	return b
}

func (b *AppBuilder) WithJWTAuth(resolver auth.IdentityResolver) *AppBuilder {
	if b.cfg.JWT.Secret == "" {
		b.errors = append(b.errors, fmt.Errorf("jwt: JWT_SECRET must not be empty"))
		return b
	}
	if len(b.cfg.JWT.Secret) < 32 {
		b.errors = append(b.errors, fmt.Errorf("jwt: JWT_SECRET must be at least 32 characters for HS256 security"))
		return b
	}
	if resolver == nil {
		b.errors = append(b.errors, fmt.Errorf("jwt: identity resolver must not be nil"))
		return b
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
	if store == nil {
		b.errors = append(b.errors, fmt.Errorf("db token auth: token store must not be nil"))
		return b
	}
	if resolver == nil {
		b.errors = append(b.errors, fmt.Errorf("db token auth: identity resolver must not be nil"))
		return b
	}
	pool := worker.New(worker.WithWorkers(2), worker.WithQueueSize(256))
	authProvider := auth.NewDBTokenProvider(store, resolver, pool)
	Bind[auth.Provider](b.container, authProvider)

	return b
}

func (b *AppBuilder) WithHttp() *AppBuilder {
	port := b.cfg.HTTP.Port
	if port == 0 {
		port = 8080
	}

	if b.cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	if b.cfg.Frontend.StaticPath != "" && b.cfg.Frontend.StaticURL != "" {
		b.engine.Static(b.cfg.Frontend.StaticURL, b.cfg.Frontend.StaticPath)
	}
	if b.spaConfig != nil {
		viteCfg := b.spaConfig.Vite
		viteCfg = viteCfg.WithDefaults()
		if viteCfg.ManifestPath != "" && viteCfg.AssetsURLPrefix != "" {
			b.engine.Static(viteCfg.AssetsURLPrefix, "frontend/dist")
		}
	}

	readHeaderTimeout := b.cfg.HTTP.ReadHeaderTimeout
	if readHeaderTimeout <= 0 {
		readHeaderTimeout = 10 * time.Second
	}
	readTimeout := b.cfg.HTTP.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 30 * time.Second
	}
	writeTimeout := b.cfg.HTTP.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = 30 * time.Second
	}
	idleTimeout := b.cfg.HTTP.IdleTimeout
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

// Build validates accumulated errors, registers SPA wiring, and
// returns the runnable *App. Returns an error if any wiring step failed.
func (b *AppBuilder) Build() (*App, error) {
	if len(b.errors) > 0 {
		return nil, fmt.Errorf("bootstrap: wiring errors: %v", b.errors)
	}

	// Apply rate limiters after all With* calls so callers can chain them in
	// any order. Global shared limiter runs first (server-wide circuit breaker),
	// then per-user limiter (individual abuse protection).
	if b.sharedGlobalLimiter != nil {
		b.engine.Use(middlewares.RateLimit(b.sharedGlobalLimiter, false, false))
	}
	if b.perUserLimiter != nil {
		b.engine.Use(middlewares.RateLimit(b.perUserLimiter, true, false))
	}

	if b.spaConfig != nil {
		log := MustResolve[*slog.Logger](b.container)
		frontend.RegisterSPAWithTemplates(b.engine, *b.spaConfig, log)
	}

	return NewApp(b.cfg, b.container, b.engine, b.server, b.modules), nil
}
