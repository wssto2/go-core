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

// OTelTracer adapts OpenTelemetry to the local Tracer interface.
type OTelTracer struct {
	tp *sdktrace.TracerProvider
	tr oteltrace.Tracer
}

// InitOpenTelemetry initializes a basic OpenTelemetry tracer provider using the
// stdout exporter (pretty print). It returns a Tracer implementation and a
// shutdown function to call at process exit.
func InitOpenTelemetry(ctx context.Context, serviceName string) (*OTelTracer, func(context.Context) error, error) {
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, nil, fmt.Errorf("creating stdout exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
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
	ot := &OTelTracer{tp: tp, tr: tp.Tracer(serviceName)}
	shutdown := func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}
	return ot, shutdown, nil
}

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
