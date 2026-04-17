package main

import (
	"context"
	"log"
	"os"
	"time"

	domainauth "go-core-example/internal/domain/auth"
	"go-core-example/internal/domain/product"

	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/go2ts"
	corei18n "github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/observability/tracing"
	"github.com/wssto2/go-core/ratelimit"
)

func main() {
	cfg := loadConfig()
	ctx := context.Background()

	// Initialize OpenTelemetry.
	// Control via environment variables:
	//   OTEL_EXPORTER=stdout   → pretty-print spans (default in dev)
	//   OTEL_EXPORTER=otlp     → send to Grafana Tempo / Jaeger / any OTLP backend
	//   OTEL_EXPORTER=noop     → discard all spans (or set DISABLE_OTEL=1)
	//   OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318  → OTLP collector URL
	if os.Getenv("DISABLE_OTEL") == "" {
		exporter := tracing.ExporterType(os.Getenv("OTEL_EXPORTER"))
		if exporter == "" {
			exporter = tracing.ExporterStdout
		}
		otelCfg := tracing.OTelConfig{
			ServiceName: cfg.App.Name,
			Exporter:    exporter,
			Endpoint:    os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		}
		if _, shutdown, err := tracing.InitOpenTelemetry(ctx, otelCfg); err != nil {
			log.Printf("tracing init failed: %v (continuing with noop tracer)", err)
		} else {
			defer func() { _ = shutdown(context.Background()) }()
		}
	}

	// Generate TypeScript types for all domain models into /tmp/go-core-example/types.
	// This runs at startup so the types directory always reflects the latest models.
	if err := go2ts.GenerateTypes(
		[]interface{}{product.Product{}, domainauth.User{}},
		"/tmp/go-core-example/types",
	); err != nil {
		log.Printf("go2ts: type generation failed: %v (non-fatal)", err)
	}

	// Build the TokenConfig so we can pass it to both WithJWTAuth and the auth
	// module (which needs it to issue tokens on login).
	tokenCfg := coreauth.TokenConfig{
		SecretKey:     cfg.JWT.Secret,
		Issuer:        cfg.JWT.Issuer,
		TokenDuration: cfg.JWT.Duration,
	}

	// Create the auth module first so we can pass its DB-backed resolver to
	// WithJWTAuth. The resolver's DB is set during Register() — before the
	// server starts accepting requests — so it is always available at request time.
	authMod := domainauth.NewModule(tokenCfg)
	productMod := product.NewModule(
		cfg.Storage.Dir,
		bootstrap.EnvStr("NOTIFICATION_WEBHOOK_URL", ""),
		bootstrap.EnvStr("NOTIFICATION_WEBHOOK_TOKEN", ""),
	)
	translator, err := corei18n.New(corei18n.Config{
		FallbackLang: cfg.I18n.DefaultLocale,
		I18nDir:      cfg.I18n.Dir,
	})
	if err != nil {
		log.Fatal(err)
	}

	pageShellComposer := newCatalogPageShellComposer(
		cfg,
		translator,
		tokenCfg,
		authMod.IdentityResolver,
		productMod.ListCatalog,
		productMod.GetCatalogProduct,
	)
	spaShellDataBuilder := newSPAShellDataBuilder(pageShellComposer)
	pageDataModule := newPageDataModule(pageShellComposer)

	app, err := bootstrap.New(cfg).
		DefaultInfrastructure().
		WithSPA(spaShellDataBuilder.Build).
		// Per-user/IP limiter: 300 req/min per identity.
		WithRateLimit(ratelimit.NewInMemoryLimiter(300, time.Minute)).
		WithJWTAuth(authMod.IdentityResolver).
		WithModules(
			authMod,
			productMod,
			pageDataModule,
		).
		WithHttp().
		Build()
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func loadConfig() bootstrap.Config {
	cfg := bootstrap.DefaultConfig()

	cfg.App.Name = bootstrap.EnvStr("APP_NAME", "go-core-example")
	cfg.App.Env = bootstrap.EnvStr("APP_ENV", "development")
	cfg.HTTP.Port = bootstrap.EnvInt("APP_PORT", 8080)
	cfg.HTTP.ReadTimeout = time.Duration(bootstrap.EnvInt("READ_TIMEOUT_SEC", 15)) * time.Second
	cfg.HTTP.WriteTimeout = time.Duration(bootstrap.EnvInt("WRITE_TIMEOUT_SEC", 15)) * time.Second
	cfg.HTTP.IdleTimeout = time.Duration(bootstrap.EnvInt("IDLE_TIMEOUT_SEC", 60)) * time.Second
	cfg.HTTP.ShutdownTimeout = time.Duration(bootstrap.EnvInt("SHUTDOWN_TIMEOUT_SEC", 10)) * time.Second

	cfg.Frontend.TemplatesPath = bootstrap.EnvStr("TEMPLATES_PATH", "frontend/templates/*.html")
	cfg.Frontend.TemplateName = bootstrap.EnvStr("TEMPLATE_NAME", "index.html")
	cfg.Frontend.StaticPath = bootstrap.EnvStr("STATIC_PATH", "")
	cfg.Frontend.StaticURL = bootstrap.EnvStr("STATIC_URL", "")
	cfg.Frontend.APIPrefix = bootstrap.EnvStr("API_PREFIX", "/api")

	// Primary database ("local") -- used by most modules.
	cfg.Database.Connections = []bootstrap.DatabaseConnectionConfig{
		{
			Name:            "local",
			Driver:          "mysql",
			Host:            bootstrap.EnvStr("DB_HOST", "127.0.0.1"),
			Port:            bootstrap.EnvStr("DB_PORT", "3306"),
			Database:        bootstrap.EnvStr("DB_DATABASE", "go_core_example"),
			Username:        bootstrap.EnvStr("DB_USERNAME", "root"),
			Password:        bootstrap.EnvStr("DB_PASSWORD", "root"),
			MaxIdleConns:    bootstrap.EnvInt("DB_MAX_IDLE_CONNS", 5),
			MaxOpenConns:    bootstrap.EnvInt("DB_MAX_OPEN_CONNS", 75),
			ConnMaxLifetime: bootstrap.EnvInt("DB_CONN_MAX_LIFETIME_MIN", 5),
		},
		// Second database ("shared") -- available to any module via c.DB("shared").
		// Uncomment and set env vars when a shared/multi-tenant DB is needed.
		// {
		// 	Name:            "shared",
		// 	Driver:          "mysql",
		// 	Host:            envStr("SHARED_DB_HOST", "127.0.0.1"),
		// 	Port:            envStr("SHARED_DB_PORT", "3306"),
		// 	Database:        envStr("SHARED_DB_DATABASE", "go_core_shared"),
		// 	Username:        envStr("SHARED_DB_USERNAME", "root"),
		// 	Password:        envStr("SHARED_DB_PASSWORD", "secret"),
		// 	MaxIdleConns:    envInt("SHARED_DB_MAX_IDLE_CONNS", 5),
		// 	MaxOpenConns:    envInt("SHARED_DB_MAX_OPEN_CONNS", 25),
		// 	ConnMaxLifetime: envInt("SHARED_DB_CONN_MAX_LIFETIME_MIN", 5),
		// },
	}

	cfg.JWT.Secret = bootstrap.EnvStr("JWT_SECRET", "change-me-to-32-bytes-minimum!!!")
	cfg.JWT.Issuer = bootstrap.EnvStr("JWT_ISSUER", "go-core-example")
	cfg.JWT.Duration = time.Duration(bootstrap.EnvInt("JWT_DURATION_HOURS", 24)) * time.Hour

	cfg.I18n.Dir = bootstrap.EnvStr("I18N_DIR", "i18n")
	cfg.I18n.DefaultLocale = bootstrap.EnvStr("I18N_FALLBACK_LANG", "en")

	cfg.Storage.Dir = bootstrap.EnvStr("STORAGE_BASE_DIR", "/tmp/go-core-example/uploads")

	return cfg
}
