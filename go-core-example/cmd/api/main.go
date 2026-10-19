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
	// A .env is optional; the real environment wins over it. Load it before gocore.New reads the environment.
	if err := gocore.LoadEnv(".env"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Fatal(err)
	}

	ctx := context.Background()

	// The SPA shell is built from the catalogue service, which exists once product is installed: the builder
	// the App runs with reads it through this variable.
	var shell spaShellDataBuilder

	// New reads APP_*, the HTTP timeouts, TRUSTED_PROXIES and LOG_* from the environment, and LOCAL_DB_* for the
	// connection declared here; a malformed value stops start-up, naming the variable.
	app := gocore.New(
		gocore.WithPrefix("/api"),
		gocore.WithDatabase("local"),
		gocore.WithSPA(func(c *gin.Context) (any, error) { return shell.Build(c), nil }),
		// Per-user limiter: 300 req/min per identity (nil: no per-IP limit).
		gocore.WithRateLimit(ratelimit.NewInMemoryLimiter(300, time.Minute), nil),
		// Deduplicates a retried POST that carries an Idempotency-Key header, for 24 hours.
		gocore.WithIdempotency(24*time.Hour),
	)
	cfg := app.Config()

	translator, err := corei18n.New(corei18n.Config{
		FallbackLang: cfg.I18n.DefaultLocale,
		I18nDir:      cfg.I18n.Dir,
	})
	if err != nil {
		log.Fatal(err)
	}

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

	catalog := product.Install(app, app.Env().String("STORAGE_BASE_DIR", "/tmp/go-core-example/uploads"))
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
