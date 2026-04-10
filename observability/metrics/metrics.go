package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds Prometheus collectors and a registry.
type Metrics struct {
	Registry        *prometheus.Registry
	requestCount    *prometheus.CounterVec
	requestErrors   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

// NewMetrics creates and registers the standard collectors.
//
// registry is the underlying *prometheus.Registry used to serve /metrics.
// registerer is used to register metrics — pass a prometheus.WrapRegistererWith
// result to attach constant labels (e.g. app="my-service") to every metric.
// When registerer is nil it falls back to registry.
// When registry is nil a fresh registry is created; registerer is then also set to it.
func NewMetrics(registry *prometheus.Registry, registerer prometheus.Registerer) *Metrics {
	if registry == nil {
		registry = prometheus.NewRegistry()
		// Register Go runtime and process collectors if we create the registry
		_ = registry.Register(collectors.NewGoCollector())
		_ = registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	}
	if registerer == nil {
		registerer = registry
	}

	requestCount := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "go_core",
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests",
	}, []string{"method", "path", "status"})

	requestErrors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "go_core",
		Name:      "http_errors_total",
		Help:      "Total number of HTTP error responses (5xx)",
	}, []string{"method", "path", "status"})

	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "go_core",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request durations in seconds",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	_ = registerer.Register(requestCount)
	_ = registerer.Register(requestErrors)
	_ = registerer.Register(requestDuration)

	return &Metrics{
		Registry:        registry,
		requestCount:    requestCount,
		requestErrors:   requestErrors,
		requestDuration: requestDuration,
	}
}

// Handler returns an HTTP handler that serves /metrics for the registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

// Middleware instruments HTTP handlers, recording request count, errors and duration.
func (m *Metrics) Middleware() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: 200}
			start := time.Now()
			next.ServeHTTP(rec, r)
			dur := time.Since(start).Seconds()
			path := r.URL.Path
			statusStr := strconv.Itoa(rec.status)
			m.requestCount.WithLabelValues(r.Method, path, statusStr).Inc()
			// Count server errors (5xx) as errors for RED metrics
			if rec.status >= 500 {
				m.requestErrors.WithLabelValues(r.Method, path, statusStr).Inc()
			}
			m.requestDuration.WithLabelValues(r.Method, path).Observe(dur)
		})
	}
}

// statusRecorder wraps http.ResponseWriter to capture response status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}
