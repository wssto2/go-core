package notification

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/observability"
)

// An application without telemetry (or with one that has no registry) counts nothing and does not panic.
func TestNoTelemetryMeansNilMetricsThatRecordNothing(t *testing.T) {
	t.Parallel()

	for _, tel := range []*observability.Telemetry{nil, {}} {
		m := newMetrics(tel)
		require.Nil(t, m)

		require.NotPanics(t, func() {
			m.create("c", inAppChannel)
			m.hold("c", pushChannel)
			m.cancel("c", pushChannel, 2)
			m.cancelAll(map[string]int64{"c": 1}, pushChannel)
			m.finished(deliveryRow{Status: statusFailed, Channel: emailChannel, failReason: reasonGone}, "c")
		})
	}
}

// Two installs on one registry share the counters instead of failing.
func TestInstallingTwiceOnOneRegistryShareTheCounters(t *testing.T) {
	t.Parallel()

	tel := observability.New(slog.New(slog.DiscardHandler), "x")

	a, b := newMetrics(tel), newMetrics(tel)
	a.create("c", inAppChannel)
	b.create("c", inAppChannel)

	require.Same(t, a.created, b.created)
}
