package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"time"

	"go-core-example/internal/domain/access"
	"go-core-example/internal/domain/product"

	"github.com/gin-gonic/gin"
	coreaccess "github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/go2ts"
	"github.com/wssto2/go-core/gocore"
	corei18n "github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/observability/tracing"
	"github.com/wssto2/go-core/ratelimit"
)

// main reads top to bottom: a feature used before the line that creates it does not compile.
//
// Run the migrations once before serving (go run ./cmd/api migrate): identity and access own their tables.
func main() {
	// A .env is optional; the real environment wins over it. Load it before the configuration is read.
	if err := gocore.LoadEnv(".env"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Fatal(err)
	}

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
		[]interface{}{product.Product{}, access.Lead{}},
		"/tmp/go-core-example/types",
	); err != nil {
		log.Printf("go2ts: type generation failed: %v (non-fatal)", err)
	}

	translator, err := corei18n.New(corei18n.Config{
		FallbackLang: cfg.I18n.DefaultLocale,
		I18nDir:      cfg.I18n.Dir,
	})
	if err != nil {
		log.Fatal(err)
	}

	// The SPA shell is built from the catalogue service, which exists once product is installed: the builder
	// the App runs with reads it through this variable.
	var shell spaShellDataBuilder

	app := gocore.New(cfg,
		gocore.WithPrefix("/api"),
		gocore.WithBuilder(func(b *bootstrap.AppBuilder) {
			b.WithSPA(func(c *gin.Context) (any, error) { return shell.Build(c), nil }).
				// Per-user/IP limiter: 300 req/min per identity.
				WithRateLimit(ratelimit.NewInMemoryLimiter(300, time.Minute)).
				// Deduplicates a retried POST that carries an Idempotency-Key header, for 24 hours.
				WithIdempotency(24 * time.Hour)
		}),
	)

	// Every permission of the application goes into one catalogue before access.Install builds the engine from it.
	catalogue := authz.NewCatalogue()
	product.DefinePermissions(catalogue)

	if err := access.DefinePermissions(catalogue); err != nil {
		log.Fatal(err)
	}

	hierarchy, err := access.NewHierarchy()
	if err != nil {
		log.Fatal(err)
	}

	roles := append(access.Roles(), authz.Role{
		Key: "merchandiser", Name: "Merchandiser",
		Grants: authz.Grants(authz.QualifierAll, product.PermCreate, product.PermUpdate, product.PermDelete),
	})

	// Sign-in, sessions, profile; also the application's authentication. WithPrincipal adds the account's own location.
	users := identity.Install(app, identity.WithoutMail(), identity.WithPrincipal(access.PrincipalOf(app.Database())))
	// Roles and bindings; also the application's authorizer.
	acc := coreaccess.Install(app, catalogue, users, coreaccess.WithRoles(roles...),
		coreaccess.WithScopes(hierarchy, access.NewPlaces(app.Database())))

	catalog := product.Install(app, cfg.Storage.Dir)
	access.Install(app, acc.Engine)

	composer := newCatalogPageShellComposer(cfg, translator, catalogList(catalog), catalogGet(catalog))
	shell = newSPAShellDataBuilder(composer)
	installPageData(app, composer)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// catalogList and catalogGet are the read-only catalogue queries the page composer takes, as plain functions.
func catalogList(svc product.Service) catalogProductListFunc {
	return func(ctx context.Context) ([]product.Product, error) {
		if svc == nil {
			return nil, errors.New("product: service not initialised")
		}

		return svc.List(ctx)
	}
}

func catalogGet(svc product.Service) catalogProductGetFunc {
	return func(ctx context.Context, id int) (product.Product, error) {
		if svc == nil {
			return product.Product{}, errors.New("product: service not initialised")
		}

		return svc.GetByID(ctx, id)
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

	cfg.I18n.Dir = bootstrap.EnvStr("I18N_DIR", "i18n")
	cfg.I18n.DefaultLocale = bootstrap.EnvStr("I18N_FALLBACK_LANG", "en")

	cfg.Storage.Dir = bootstrap.EnvStr("STORAGE_BASE_DIR", "/tmp/go-core-example/uploads")

	return cfg
}
