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

func New(log *slog.Logger) *Telemetry {
	reg := prometheus.NewRegistry()

	// Register Go runtime and process metrics
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return &Telemetry{
		Registry: reg,
		HTTP:     metrics.NewMetrics(reg),
		Service:  NewServiceMiddleware(log, reg),
		Worker:   worker.NewManagerMetrics(reg),
	}
}
