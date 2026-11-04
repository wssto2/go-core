//nolint:testpackage // reads App.startTracing, which is called inside Run
package gocore

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithTracingStartsFromTheEnvironmentAndSkipsWhatShouldNot(t *testing.T) {
	var logs bytes.Buffer

	app := func(opts ...Option) *App {
		return New(append([]Option{WithLogger(slog.New(slog.NewTextHandler(&logs, nil)))}, opts...)...)
	}

	t.Setenv("OTEL_EXPORTER", "noop")

	// Not asked for: nothing starts, and the test would fail on a nil call.
	assert.NotPanics(t, app().startTracing(t.Context()))
	assert.False(t, app().tracing)

	// Asked for: it starts, and the returned function flushes it.
	on := app(WithTracing())
	assert.True(t, on.tracing)
	assert.NotPanics(t, on.startTracing(t.Context()))

	// The init error path is a log line; the OTel exporters fail on nothing a test can set, so it is not exercised.
	t.Setenv("OTEL_EXPORTER", "otlp")

	// DISABLE_OTEL and an offline App skip it: no start: the otlp exporter would log its endpoint.
	logs.Reset()
	t.Setenv("DISABLE_OTEL", "1")
	app(WithTracing()).startTracing(t.Context())()
	app(WithTracing(), Offline()).startTracing(t.Context())()
	t.Setenv("DISABLE_OTEL", "")
	app(WithTracing(), Offline()).startTracing(t.Context())()
	assert.NotContains(t, logs.String(), "tracing_")
}
