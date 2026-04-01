// bootstrap/observability/service_middleware.go

package observability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ServiceObserver wraps any method call with automatic logging and metrics.
// Google calls this "interceptors", Uber calls this "middleware".
type ServiceObserver struct {
	logger    *slog.Logger
	histogram *prometheus.HistogramVec
	errors    *prometheus.CounterVec
}

func NewServiceMiddleware(logger *slog.Logger, reg prometheus.Registerer) *ServiceObserver {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	histogram := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "service_operation_duration_seconds",
		Help:    "Duration of service operations.",
		Buckets: prometheus.DefBuckets,
	}, []string{"service", "operation", "status"})

	errors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "service_operation_errors_total",
		Help: "Total number of service operation errors.",
	}, []string{"service", "operation"})

	reg.MustRegister(histogram, errors)

	return &ServiceObserver{logger: logger, histogram: histogram, errors: errors}
}

// Do wraps a single operation with logging, metrics, and panic recovery.
// Call this from generated or hand-written service wrappers.
func (m *ServiceObserver) Do(
	ctx context.Context,
	service, operation string,
	fn func(ctx context.Context) error,
) error {
	start := time.Now()
	log := m.logger.With("service", service, "operation", operation)
	log.DebugContext(ctx, "operation started")

	err := m.safeCall(ctx, fn)

	duration := time.Since(start)
	status := "ok"
	if err != nil {
		status = "error"
		m.errors.WithLabelValues(service, operation).Inc()
		log.ErrorContext(ctx, "operation failed",
			"error", err,
			"duration_ms", duration.Milliseconds(),
		)
	} else {
		log.DebugContext(ctx, "operation completed",
			"duration_ms", duration.Milliseconds(),
		)
	}

	m.histogram.WithLabelValues(service, operation, status).Observe(duration.Seconds())
	return err
}

func (m *ServiceObserver) safeCall(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn(ctx)
}
