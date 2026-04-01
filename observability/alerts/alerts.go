package alerts

import (
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// Alerting provides minimal metrics and knobs to make the service "alert-ready".
// It registers two gauges:
// - go_core_alerting_enabled{service="..."} (1 = enabled, 0 = disabled)
// - go_core_service_up{service="..."} (1 = up, 0 = down)
//
// New reads the ALERTING_ENABLED env var to determine the initial enabled state
// (default: enabled).
type Alerting struct {
	service string
	enabled *prometheus.GaugeVec
	up      *prometheus.GaugeVec
	reg     *prometheus.Registry
}

// New creates an Alerting instance registering metrics on the provided registry.
// If registry is nil a new Prometheus registry is created.
func New(registry *prometheus.Registry, serviceName string) *Alerting {
	if registry == nil {
		registry = prometheus.NewRegistry()
	}

	enabledVec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "go_core",
		Name:      "alerting_enabled",
		Help:      "Whether alerting is enabled for this service (1=enabled,0=disabled)",
	}, []string{"service"})

	upVec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "go_core",
		Name:      "service_up",
		Help:      "Service health (1=up,0=down)",
	}, []string{"service"})

	// Best-effort registration; ignore errors to allow composed registries in tests.
	_ = registry.Register(enabledVec)
	_ = registry.Register(upVec)

	a := &Alerting{
		service: serviceName,
		enabled: enabledVec,
		up:      upVec,
		reg:     registry,
	}

	// Determine initial enabled state from env var ALERTING_ENABLED (default true)
	val := strings.ToLower(strings.TrimSpace(os.Getenv("ALERTING_ENABLED")))
	enabled := true
	if val == "0" || val == "false" || val == "no" || val == "off" {
		enabled = false
	}
	// defaults: enabled per env; up=true
	a.SetEnabled(enabled)
	a.SetUp(true)

	return a
}

// SetEnabled toggles whether alerting is enabled (gauge 1/0).
func (a *Alerting) SetEnabled(enabled bool) {
	v := 0.0
	if enabled {
		v = 1.0
	}
	a.enabled.WithLabelValues(a.service).Set(v)
}

// SetUp sets the service health gauge (1 = up, 0 = down).
func (a *Alerting) SetUp(up bool) {
	v := 0.0
	if up {
		v = 1.0
	}
	a.up.WithLabelValues(a.service).Set(v)
}

// Registry returns the underlying Prometheus registry.
func (a *Alerting) Registry() *prometheus.Registry { return a.reg }
