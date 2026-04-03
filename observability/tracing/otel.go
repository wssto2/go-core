package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// ExporterType selects the span exporter used by InitOpenTelemetry.
type ExporterType string

const (
	// ExporterStdout pretty-prints spans to stdout. Suitable for development.
	ExporterStdout ExporterType = "stdout"
	// ExporterNoop discards all spans. Suitable for production when a real
	// backend (e.g. OTLP) is not yet configured.
	ExporterNoop ExporterType = "noop"
)

// OTelConfig holds the configuration for InitOpenTelemetry.
type OTelConfig struct {
	// ServiceName is used as the OTel resource service.name attribute.
	ServiceName string
	// Exporter selects the span exporter. Defaults to ExporterNoop when empty.
	Exporter ExporterType
}

// OTelTracer adapts OpenTelemetry to the local Tracer interface.
type OTelTracer struct {
	tp *sdktrace.TracerProvider
	tr oteltrace.Tracer
}

// InitOpenTelemetry initialises an OpenTelemetry tracer provider.
// The exporter is selected via cfg.Exporter:
//   - "stdout" — pretty-prints spans to stdout (development)
//   - "noop" or "" — discards all spans (safe production default)
//
// OTLP exporters are not yet included; add them once the corresponding
// go.opentelemetry.io/otel/exporters/otlp/* packages are in go.mod.
func InitOpenTelemetry(ctx context.Context, cfg OTelConfig) (*OTelTracer, func(context.Context) error, error) {
	exporter, err := buildExporter(cfg.Exporter)
	if err != nil {
		return nil, nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("creating resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	ot := &OTelTracer{tp: tp, tr: tp.Tracer(cfg.ServiceName)}
	shutdown := func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}
	return ot, shutdown, nil
}

func buildExporter(t ExporterType) (sdktrace.SpanExporter, error) {
	switch t {
	case ExporterStdout:
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	default: // ExporterNoop or empty
		return &noopExporter{}, nil
	}
}

// noopExporter discards all spans.
type noopExporter struct{}

func (*noopExporter) ExportSpans(_ context.Context, _ []sdktrace.ReadOnlySpan) error { return nil }
func (*noopExporter) Shutdown(_ context.Context) error                               { return nil }


// StartSpan implements Tracer. It starts an OpenTelemetry span and stores the
// trace/span ids in the returned context so other parts of the system can
// access them via TraceIDFromContext / SpanIDFromContext.
func (o *OTelTracer) StartSpan(ctx context.Context, name string) (context.Context, func(err error)) {
	ctx2, span := o.tr.Start(ctx, name)
	// capture span context and expose trace/span ids on returned context
	sc := oteltrace.SpanContextFromContext(ctx2)
	if sc.IsValid() {
		ctx2 = context.WithValue(ctx2, traceIDKey, sc.TraceID().String())
		ctx2 = context.WithValue(ctx2, spanIDKey, sc.SpanID().String())
	}

	finish := func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
	return ctx2, finish
}
