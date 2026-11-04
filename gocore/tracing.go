package gocore

import (
	"context"
	"time"

	"github.com/wssto2/go-core/observability/tracing"
)

// tracingShutdownWait bounds the flush of the last spans when the process ends.
const tracingShutdownWait = 5 * time.Second

// WithTracing starts OpenTelemetry tracing when Run does and flushes it when Run ends. It reads, through Env:
//
//	DISABLE_OTEL=1                          no tracer
//	OTEL_EXPORTER=stdout|otlp|noop          the span exporter; stdout when unset
//	OTEL_EXPORTER_OTLP_ENDPOINT=http://...  the collector, for otlp
//
// An offline App (ts:generate) never starts it, so a generator touches no network. A tracer that cannot start is
// logged and the application goes on without one.
func WithTracing() Option {
	return func(app *App) { app.tracing = true }
}

// startTracing starts the tracer WithTracing asked for and returns what ends it; it returns a no-op when none runs.
func (a *App) startTracing(ctx context.Context) func() {
	none := func() {}

	if !a.tracing || a.offline || a.env.String("DISABLE_OTEL", "") != "" {
		return none
	}

	exporter := tracing.ExporterType(a.env.String("OTEL_EXPORTER", string(tracing.ExporterStdout)))

	_, shutdown, err := tracing.InitOpenTelemetry(ctx, tracing.OTelConfig{
		ServiceName: a.name,
		Exporter:    exporter,
		Endpoint:    a.env.String("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
	})
	if err != nil {
		a.log.Error("tracing_init_failed", "error", err, "exporter", string(exporter), "continuing", "with no tracer")

		return none
	}

	return func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracingShutdownWait)
		defer cancel()

		err := shutdown(flush)
		if err != nil {
			a.log.Error("tracing_shutdown_failed", "error", err)
		}
	}
}
