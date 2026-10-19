package gocore

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/frontend"
	"github.com/wssto2/go-core/health"
	"github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/logger"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/observability"
	"github.com/wssto2/go-core/observability/metrics"
	"github.com/wssto2/go-core/ratelimit"
	"github.com/wssto2/go-core/web"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// appBuilder constructs an App using a fluent chain of calls.
type appBuilder struct {
	cfg                 Config
	prefix              string // the API prefix, handed to the SPA so its catch-all leaves the API alone
	engine              *gin.Engine
	modules             []component
	translator          *i18n.Translator       // built from cfg.I18n, nil when it names no directory
	health              *health.HealthRegistry // built by DefaultInfrastructure
	server              httpServer
	errors              []error // accumulate wiring errors, report all at once in build
	perUserLimiter      ratelimit.Limiter
	ipLimiter           ratelimit.Limiter
	ipLimiterPrefix     string
	sharedGlobalLimiter ratelimit.Limiter
	spaConfig           *frontend.SPAConfig
	trustedOrigins      []string
	idempotencyStore    *middlewares.IdempotencyStore
	registry            *database.Registry       // supplied with WithRegistry, else opened from cfg
	log                 *slog.Logger             // supplied with WithLogger, else built from cfg
	telemetry           *observability.Telemetry // supplied with WithTelemetry, else built from cfg
	engineMiddleware    []gin.HandlerFunc        // supplied with Use, applied by Build
	csp                 []gin.HandlerFunc        // supplied with WithCSP, applied by Build
}

// New creates a new appBuilder with the given config.
func newBuilder(cfg Config) *appBuilder {
	engine := gin.New()
	builder := &appBuilder{
		cfg:    cfg,
		engine: engine,
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

// DefaultInfrastructure builds the core services (logger, database registry,
// i18n, telemetry, health) and registers the infrastructure HTTP routes. Call
// this first, before WithComponents.
func (b *appBuilder) DefaultInfrastructure() *appBuilder {
	b.setupLogger()
	b.setupI18n()
	b.setupObservability()
	b.setupHealth()
	b.setupHTTPMiddlewares()

	return b
}

func (b *appBuilder) setupLogger() {
	if b.log != nil {
		return
	}

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

	b.log = log
}

func registryConfig(cfg DatabaseConfig) database.RegistryConfig {
	return database.RegistryConfig{
		LogLevel:           cfg.LogLevel,
		SlowQueryThreshold: cfg.SlowQueryThreshold,
		LogParameters:      cfg.LogParameters,
	}
}

func connectionConfigs(cfg DatabaseConfig) []database.ConnectionConfig {
	connections := make([]database.ConnectionConfig, 0, len(cfg.Connections))
	for _, conn := range cfg.Connections {
		connections = append(connections, database.ConnectionConfig{
			Name:            conn.Name,
			Driver:          conn.Driver,
			Host:            conn.Host,
			Port:            conn.Port,
			Database:        conn.Database,
			Username:        conn.Username,
			Password:        conn.Password,
			SQLMode:         conn.SQLMode,
			Charset:         conn.Charset,
			MaxIdleConns:    conn.MaxIdleConns,
			MaxOpenConns:    conn.MaxOpenConns,
			ConnMaxLifetime: conn.ConnMaxLifetime,
			Debug:           conn.Debug,
		})
	}

	return connections
}

// openDatabase opens every connection of cfg into a Registry and returns the
// first failure as an error. Pass the result to appBuilder.WithRegistry.
func openDatabase(log *slog.Logger, cfg DatabaseConfig) (*database.Registry, error) {
	reg := database.NewRegistry(log, registryConfig(cfg))

	for _, conn := range connectionConfigs(cfg) {
		if err := reg.Register(conn); err != nil {
			_ = reg.CloseAll()
			return nil, err
		}
	}

	return reg, nil
}

// WithRegistry makes the application use reg instead of opening the
// connections listed in the config. The caller closes it.
func (b *appBuilder) WithRegistry(reg *database.Registry) *appBuilder {
	b.registry = reg
	return b
}

// WithLogger makes the application use log instead of building one from the
// log config.
func (b *appBuilder) WithLogger(log *slog.Logger) *appBuilder {
	b.log = log
	return b
}

// WithTelemetry makes the application use tel instead of building its own,
// for a host that needs the handle before the builder runs. Call it before
// DefaultInfrastructure.
func (b *appBuilder) WithTelemetry(tel *observability.Telemetry) *appBuilder {
	b.telemetry = tel

	return b
}

func (b *appBuilder) setupI18n() {
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

	b.translator = t
}

func (b *appBuilder) setupObservability() {
	tel := b.telemetry
	if tel == nil {
		tel = observability.New(b.log, b.cfg.App.Name)
		b.telemetry = tel
	}

	// Expose /metrics using the app's own registry — not the global default
	b.engine.GET("/metrics", gin.WrapH(
		promhttp.HandlerFor(tel.Registry, promhttp.HandlerOpts{}),
	))
}

func (b *appBuilder) setupHealth() {
	b.health = mountHealth(b.engine, b.registry)
}

// mountHealth serves /health (liveness) and /ready (a ping of the primary connection, when there is one) on engine.
func mountHealth(engine gin.IRouter, reg *database.Registry) *health.HealthRegistry {
	hr := health.NewHealthRegistry()

	if reg != nil && reg.PrimaryName() != "" {
		hr.Add(health.NewDBHealthChecker(reg.Primary()))
	}

	engine.GET("/health", health.LivenessHandler())
	engine.GET("/ready", health.ReadinessHandler(hr))

	return hr
}

func (b *appBuilder) setupHTTPMiddlewares() {
	log := b.log
	tel := b.telemetry
	translator := b.translator

	b.engine.Use(
		otelgin.Middleware(b.cfg.App.Name),
		middlewares.RequestID(),
		middlewares.RequestLogger(log),
		metrics.InstrumentHTTP(tel.HTTP),
		middlewares.PanicRecovery(log),
		middlewares.ErrorHandler(log, translator, b.cfg.App.Development()),
		web.BodyLimit(b.cfg.HTTP.BodyLimit()),
		middlewares.Security(b.cfg.App.Development(), securityConfig(b.cfg)),
		middlewares.Cors(middlewares.CorsConfig{
			AllowOrigins: b.cfg.CORS.AllowedOrigins,
			AllowMethods: b.cfg.CORS.AllowedMethods,
			AllowHeaders: b.cfg.CORS.AllowedHeaders,
		}),
	)

}

// securityConfig lets Security trust X-Forwarded-Proto for HSTS when proxies are trusted.
func securityConfig(cfg Config) middlewares.SecurityConfig {
	return middlewares.SecurityConfig{ContentSecurityPolicy: "", TrustProxy: len(cfg.HTTP.TrustedProxies) > 0}
}

// WithRateLimit limits each authenticated user: every user gets its own
// request bucket, keyed by user ID.
//
// It is enforced by auth.Authenticated right after the identity is resolved
// (auth.DeferUserRateLimit). An engine-level limiter runs before the route
// groups' authentication and never sees the user, so the previous "per user"
// key fell back to the client IP for every request and everyone behind one NAT
// shared a bucket. Routes without Authenticated are not counted here: add
// WithIPRateLimit for anonymous traffic (login, public endpoints).
func (b *appBuilder) WithRateLimit(l ratelimit.Limiter) *appBuilder {
	b.perUserLimiter = l
	return b
}

// WithIPRateLimit limits requests per client IP on the API: paths under
// pathPrefix, "/api/" when omitted. The SPA shell and static assets are left
// alone, so a limited browser still loads the page instead of a JSON 429 body.
//
// Size the ceiling for a whole office behind one NAT: it is a guard against a
// single flooding machine and brute force on anonymous endpoints, while
// WithRateLimit budgets each signed-in user.
func (b *appBuilder) WithIPRateLimit(l ratelimit.Limiter, pathPrefix ...string) *appBuilder {
	b.ipLimiter = l
	b.ipLimiterPrefix = "/api/"

	if len(pathPrefix) > 0 {
		b.ipLimiterPrefix = pathPrefix[0]
	}

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
func (b *appBuilder) WithGlobalRateLimit(l ratelimit.Limiter) *appBuilder {
	b.sharedGlobalLimiter = l
	return b
}

// WithIdempotency enables HTTP-level request deduplication for every route.
// Requests that carry an Idempotency-Key header and share the same key run
// the handler once; concurrent duplicates block until the first finishes and
// then receive its captured response, and later duplicates within ttl replay
// it without re-running the handler. Requests without the header are
// unaffected, so this is safe to apply globally — clients opt in per request.
//
// Chain it before WithComponents so the middleware is in place when
// components register their routes.
func (b *appBuilder) WithIdempotency(ttl time.Duration) *appBuilder {
	b.idempotencyStore = middlewares.NewInMemoryIdempotencyStore(ttl)

	return b
}

// Use adds engine middleware that runs before every route, those of modules
// included, after the builder's own (request id, logging, security, rate
// limits) and in the order given. It is applied by Build, so it may be called
// at any point of the chain.
func (b *appBuilder) Use(middleware ...gin.HandlerFunc) *appBuilder {
	b.engineMiddleware = append(b.engineMiddleware, middleware...)

	return b
}

// WithComponents adds the components the App registers, boots and shuts down,
// in this order and its reverse.
func (b *appBuilder) WithComponents(components ...component) *appBuilder {
	b.modules = append(b.modules, components...)
	return b
}

// WithSPA enables the convention-based SPA setup for applications that keep
// their frontend files under ./frontend. The provided state builder is optional
// and is injected into the rendered template as appState.
func (b *appBuilder) WithSPA(stateBuilder frontend.AppStateBuilder) *appBuilder {
	cfg := frontend.SPAConfig{
		TemplatesPath: b.cfg.Frontend.TemplatesPath,
		TemplateName:  b.cfg.Frontend.TemplateName,
		APIPrefix:     b.prefix,
		StateBuilder:  stateBuilder,
		DevMode:       b.cfg.App.Development(),
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
func (b *appBuilder) WithSPAConfig(cfg frontend.SPAConfig) *appBuilder {
	if cfg.APIPrefix == "" {
		cfg.APIPrefix = b.prefix
	}

	if cfg.APIPrefix == "" {
		cfg.APIPrefix = "/api"
	}

	if !cfg.DevMode {
		cfg.DevMode = b.cfg.App.Development()
	}

	b.spaConfig = &cfg

	return b
}

// WithTrustedOrigins registers HTTP origins that are allowed to load resources
// from this app (img-src) and embed it in iframes (frame-ancestors). Pass full
// URLs or bare origins — only the scheme and host are used.
func (b *appBuilder) WithTrustedOrigins(origins ...string) *appBuilder {
	b.trustedOrigins = append(b.trustedOrigins, origins...)
	return b
}

// WithCSP adds sources to a directive of the Content-Security-Policy, and
// creates the directive when the policy has none. Sources are written as in a
// policy, quotes included; production keeps its nonce-only script-src.
func (b *appBuilder) WithCSP(directive string, sources ...string) *appBuilder {
	b.csp = append(b.csp, middlewares.CSPSources(directive, sources...))

	return b
}

// WithTrustedProxies configures the proxies Gin will trust for client-IP and
// forwarded-proto resolution. Passing no values disables proxy trust.
func (b *appBuilder) WithTrustedProxies(proxies ...string) *appBuilder {
	if len(proxies) == 0 {
		proxies = nil
	}

	b.cfg.HTTP.TrustedProxies = append([]string(nil), proxies...)
	if err := b.engine.SetTrustedProxies(proxies); err != nil {
		b.errors = append(b.errors, fmt.Errorf("trusted proxies: %w", err))
	}

	return b
}

func (b *appBuilder) WithHttp() *appBuilder {
	port := b.cfg.HTTP.Port
	if port == 0 {
		port = 8080
	}

	if !b.cfg.App.Development() {
		gin.SetMode(gin.ReleaseMode)
	}

	if b.cfg.Frontend.StaticPath != "" && b.cfg.Frontend.StaticURL != "" {
		serveStatic(b.engine, b.cfg.Frontend.StaticURL, b.cfg.Frontend.StaticPath)
	}
	if b.spaConfig != nil {
		viteCfg := b.spaConfig.Vite
		viteCfg = viteCfg.WithDefaults()
		if viteCfg.ManifestPath != "" && viteCfg.AssetsURLPrefix != "" {
			// Derive the assets directory from the manifest path:
			// e.g. "./frontend/dist/.vite/manifest.json" → "./frontend/dist"
			assetsDir := filepath.Dir(filepath.Dir(viteCfg.ManifestPath))
			serveStatic(b.engine, viteCfg.AssetsURLPrefix, assetsDir)
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
	b.server = newServerWrapper(srv, b.cfg.HTTP.shutdownGracePeriod())

	return b
}

// Build validates accumulated errors, registers SPA wiring, and
// returns the runnable *lifecycle. Returns an error if any wiring step failed.
func (b *appBuilder) Build() (*lifecycle, error) {
	if len(b.errors) > 0 {
		return nil, fmt.Errorf("wiring errors: %v", b.errors)
	}

	// Idempotency runs ahead of the rate limiters so a replayed response
	// short-circuits before consuming any request budget.
	if b.idempotencyStore != nil {
		b.engine.Use(middlewares.Idempotency(b.idempotencyStore))
	}

	// Apply rate limiters after all With* calls so callers can chain them in
	// any order. Global shared limiter runs first (server-wide circuit breaker),
	// then the per-IP API guard, then the per-user limit (armed here, enforced
	// by auth.Authenticated).
	if b.sharedGlobalLimiter != nil {
		b.engine.Use(middlewares.RateLimit(b.sharedGlobalLimiter, false, false))
	}
	if b.ipLimiter != nil {
		b.engine.Use(middlewares.IPRateLimit(b.ipLimiter, b.ipLimiterPrefix))
	}
	// Enforced by auth.Authenticated once the user is known (per user ID).
	if b.perUserLimiter != nil {
		b.engine.Use(auth.DeferUserRateLimit(b.perUserLimiter))
	}

	// TrustedOriginsMiddleware patches the CSP set by Security and runs inside
	// Security's ctx.Next() call, so headers are updated before the handler
	// writes the response body.
	if len(b.trustedOrigins) > 0 {
		b.engine.Use(middlewares.TrustedOriginsMiddleware(b.trustedOrigins...))
	}

	if len(b.csp) > 0 {
		b.engine.Use(b.csp...)
	}

	if len(b.engineMiddleware) > 0 {
		b.engine.Use(b.engineMiddleware...)
	}

	if b.spaConfig != nil {
		frontend.RegisterSPAWithTemplates(b.engine, *b.spaConfig, b.log)
	}

	return newLifecycle(b.cfg, b.log, b.health, b.engine, b.server, b.modules), nil
}
