package notification

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/wssto2/go-core/observability"
)

// The channel label of the notification metrics: the inbox entry itself, and the two deliveries beside it.
const inAppChannel = "in_app"

// The reason label of go_core_notification_failed_total. The set is closed: it is never an error text.
const (
	reasonGone      = "gone"      // push: the push service said the subscription no longer exists (404, 410)
	reasonRejected  = "rejected"  // push: the push service refused the message for good (another 4xx)
	reasonPermanent = "permanent" // e-mail: the relay said the address or message can never be sent
	reasonExpired   = "expired"   // the retries ran past the delivery's lifetime (NOTIF-DELIVERY-001)
)

// unknownCategory is the category label of a delivery whose notification was deleted before the delivery ended.
const unknownCategory = "unknown"

// metrics are the Prometheus counters of the module, by category (the application's declared kinds, the module's
// own system.test and unknown) and channel (in_app, email, push). A nil *metrics records nothing: an application
// without telemetry behaves as it always did.
type metrics struct {
	created, sent, cancelled, held *prometheus.CounterVec
	failed                         *prometheus.CounterVec
}

// newMetrics registers the counters on the application's telemetry; it returns nil, and nothing is counted, when
// the application has none. Installing twice on one registry shares the counters.
func newMetrics(tel *observability.Telemetry) *metrics {
	if tel == nil || (tel.Registerer == nil && tel.Registry == nil) {
		return nil
	}

	reg := tel.Registerer
	if reg == nil {
		reg = tel.Registry
	}

	counter := func(name, help string, extra ...string) *prometheus.CounterVec {
		c := prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "go_core", Subsystem: "notification", Name: name, Help: help,
		}, append([]string{"category", "channel"}, extra...))

		var already prometheus.AlreadyRegisteredError
		if err := reg.Register(c); errors.As(err, &already) {
			return already.ExistingCollector.(*prometheus.CounterVec) //nolint:forcetypeassert // registered by this function
		}

		return c
	}

	return &metrics{
		created:   counter("created_total", "Notifications made (channel in_app) and deliveries planned beside them (email, push)."),
		sent:      counter("sent_total", "Deliveries the mail relay or the push service accepted."),
		failed:    counter("failed_total", "Deliveries given up on, by reason: gone, rejected, permanent, expired.", "reason"),
		cancelled: counter("cancelled_total", "Deliveries ended without being sent: read first, notification deleted, device removed or disabled."),
		held:      counter("held_total", "Deliveries made to wait for the end of the person's quiet hours."),
	}
}

func (m *metrics) enabled() bool { return m != nil }

func (m *metrics) create(category, channel string) {
	if m != nil {
		m.created.WithLabelValues(category, channel).Inc()
	}
}

func (m *metrics) hold(category, channel string) {
	if m != nil {
		m.held.WithLabelValues(category, channel).Inc()
	}
}

func (m *metrics) cancel(category, channel string, n int64) {
	if m != nil && n > 0 {
		m.cancelled.WithLabelValues(category, channel).Add(float64(n))
	}
}

// finished counts the end of a delivery by its status.
func (m *metrics) finished(d deliveryRow, category string) {
	if m == nil {
		return
	}

	switch d.Status {
	case statusSent:
		m.sent.WithLabelValues(category, d.Channel).Inc()
	case statusFailed:
		m.failed.WithLabelValues(category, d.Channel, d.failReason).Inc()
	case statusCancelled:
		m.cancelled.WithLabelValues(category, d.Channel).Inc()
	}
}

// noteCreated counts what a committed Send wrote: the notifications, and the deliveries beside them, those in
// quiet hours as held too.
func (n *Notices) noteCreated(created []row, made []deliveryRow) {
	for _, r := range created {
		n.metrics.create(r.Category, inAppChannel)
	}

	for _, d := range made {
		n.metrics.create(d.category, d.Channel)

		if d.Status == statusHeld {
			n.metrics.hold(d.category, d.Channel)
		}
	}
}

// cancelAll counts deliveries of a channel ended in bulk, by category.
func (m *metrics) cancelAll(byCategory map[string]int64, channel string) {
	for category, n := range byCategory {
		m.cancel(category, channel, n)
	}
}
