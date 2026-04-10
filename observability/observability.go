// observability/observability.go
package observability

import (
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/wssto2/go-core/observability/metrics"
	"github.com/wssto2/go-core/worker"
)

// Telemetry is the single observability handle passed through the
// application. It owns all metric registrations.
type Telemetry struct {
	Registry *prometheus.Registry
	HTTP     *metrics.Metrics
	Service  *ServiceObserver
	Worker   *worker.ManagerMetrics
}

// New initialises Telemetry for the application.
//
// appName is added as a constant app label to every metric so that multiple
// go-core services sharing the same Prometheus instance can be distinguished:
//
//	go_core_http_errors_total{app="order-service", ...}
//	go_core_http_errors_total{app="product-service", ...}
//
// Pass an empty string to skip the label (e.g. in unit tests).
func New(log *slog.Logger, appName string) *Telemetry {
	reg := prometheus.NewRegistry()

	// labeled is a prometheus.Registerer that wraps reg and stamps every
	// registered metric with a constant app label.
	var labeled prometheus.Registerer = reg
	if appName != "" {
		labeled = prometheus.WrapRegistererWith(prometheus.Labels{"app": appName}, reg)
	}

	// Register Go runtime and process metrics through the labeled registerer
	// so they also carry the app label.
	labeled.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return &Telemetry{
		Registry: reg,
		HTTP:     metrics.NewMetrics(reg, labeled),
		Service:  NewServiceMiddleware(log, labeled),
		Worker:   worker.NewManagerMetrics(labeled),
	}
}
