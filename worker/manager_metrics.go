// worker/manager_metrics.go
package worker

import (
	"github.com/prometheus/client_golang/prometheus"
)

type ManagerMetrics struct {
	workerPanics   *prometheus.CounterVec
	workerRestarts *prometheus.CounterVec
	workerErrors   *prometheus.CounterVec
}

func NewManagerMetrics(reg prometheus.Registerer) *ManagerMetrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	panics := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "worker_panics_total",
		Help: "Total worker panics recovered.",
	}, []string{"worker"})

	restarts := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "worker_restarts_total",
		Help: "Total worker restarts.",
	}, []string{"worker"})

	errors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "worker_errors_total",
		Help: "Total worker run errors.",
	}, []string{"worker"})

	reg.MustRegister(panics, restarts, errors)
	return &ManagerMetrics{workerPanics: panics, workerRestarts: restarts, workerErrors: errors}
}
