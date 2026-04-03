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

	// Initialize OpenTelemetry when available.
	// Use ExporterStdout for local development; switch to ExporterNoop (or OTLP)
	// in production via config.
	if os.Getenv("DISABLE_OTEL") == "" {
		otelCfg := tracing.OTelConfig{
			ServiceName: cfg.AppName,
			Exporter:    tracing.ExporterStdout,
		}
		if _, shutdown, err := tracing.InitOpenTelemetry(ctx, otelCfg); err != nil {
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
