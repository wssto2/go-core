package main

import (
	"context"
	"log"
	"os"

	"go-core-example/internal/domain/auth"
	"go-core-example/internal/domain/product"

	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/observability/tracing"

	"github.com/prometheus/client_golang/prometheus"
)

func main() {
	cfg := loadConfig()
	ctx := context.Background()

	// Initialize OpenTelemetry (stdout exporter) when available. This is
	// optional for local development; set DISABLE_OTEL=1 to skip.
	if os.Getenv("DISABLE_OTEL") == "" {
		if _, shutdown, err := tracing.InitOpenTelemetry(ctx, cfg.AppName); err != nil {
			log.Printf("tracing init failed: %v (continuing with noop tracer)", err)
		} else {
			defer func() { _ = shutdown(context.Background()) }()
		}
	}

	app, err := bootstrap.New(cfg).
		DefaultInfrastructure().
		WithJWTAuth(auth.IdentityResolver).
		WithModules(product.NewModule()).
		WithHttp().
		Build()
	if err != nil {
		productErrorCounter := prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "go_core_example_errors_total",
				Help: "Total number of errors in go-core-example",
			},
			[]string{"module", "error_type"},
		)
		prometheus.MustRegister(productErrorCounter)
		productErrorCounter.WithLabelValues("main", "bootstrap_build").Inc()
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		productErrorCounter := prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "go_core_example_errors_total",
				Help: "Total number of errors in go-core-example",
			},
			[]string{"module", "error_type"},
		)
		prometheus.MustRegister(productErrorCounter)
		productErrorCounter.WithLabelValues("main", "app_run").Inc()
		log.Fatal(err)
	}
}
