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

	app, err := bootstrap.New(cfg).
		DefaultInfrastructure().
		// Per-user/IP limiter: 300 req/min per identity.
		WithRateLimit(ratelimit.NewInMemoryLimiter(300, time.Minute)).
		WithJWTAuth(authMod.IdentityResolver).
		WithModules(
			authMod,
			product.NewModule(
				cfg.Storage.Dir,
				bootstrap.EnvStr("NOTIFICATION_WEBHOOK_URL", ""),
				bootstrap.EnvStr("NOTIFICATION_WEBHOOK_TOKEN", ""),
			),
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
