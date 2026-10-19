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

	for _, tel := range []*observability.Telemetry{nil, new(observability.Telemetry)} {
		nothing := newMetrics(tel)
		require.Nil(t, nothing)

		require.NotPanics(t, func() {
			nothing.create("c", inAppChannel)
			nothing.hold("c", pushChannel)
			nothing.cancel("c", pushChannel, 2)
			nothing.cancelPushes(map[string]int64{"c": 1})
			//nolint:exhaustruct,exhaustruct_v5 // zero by design
			nothing.finished(deliveryRow{Status: statusFailed, Channel: emailChannel, failReason: reasonGone, category: "c"})
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
