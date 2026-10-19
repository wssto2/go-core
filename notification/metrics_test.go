package notification_test

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/mail"
	"github.com/wssto2/go-core/notification"
	"gorm.io/gorm"
)

const (
	catAssigned = "tickets.assigned"
	catPushed   = "tickets.pushed"
)

// counters is every notification counter of the application's registry as "name{label=value,...}" → value; the app
// label is left out.
func counters(t *testing.T, w *world) map[string]float64 {
	t.Helper()

	families, err := w.app.Telemetry().Registry.Gather()
	require.NoError(t, err)

	out := map[string]float64{}

	for _, f := range families {
		if !strings.HasPrefix(f.GetName(), "go_core_notification_") {
			continue
		}

		for _, m := range f.GetMetric() {
			var labels []string

			for _, l := range m.GetLabel() {
				if l.GetName() != "app" {
					labels = append(labels, l.GetName()+"="+l.GetValue())
				}
			}

			sort.Strings(labels)
			out[strings.TrimPrefix(f.GetName(), "go_core_notification_")+"{"+strings.Join(labels, ",")+"}"] = m.GetCounter().GetValue()
		}
	}

	return out
}

func key(name, category, channel string, extra ...string) string {
	labels := append([]string{"category=" + category, "channel=" + channel}, extra...)
	sort.Strings(labels)

	return name + "{" + strings.Join(labels, ",") + "}"
}

// Each outcome of an e-mail adds to its own counter and no other.
func TestEmailOutcomesCountExactlyTheirOwnCounter(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		loc := zagreb(t)
		w := newMailWorld(t, db, notification.TimeZone(loc))
		atNoon(w, loc)
		w.consumeAssigned()
		w.start()

		require.Empty(t, counters(t, w))

		w.publish(assignedEvent{TicketID: 7, To: []int{1}})
		require.NoError(t, w.drain())

		require.Equal(t, map[string]float64{
			key("created_total", catAssigned, "in_app"): 1,
			key("created_total", catAssigned, "email"):  1,
		}, counters(t, w))

		require.Equal(t, 1, w.deliverDue())

		require.Equal(t, map[string]float64{
			key("created_total", catAssigned, "in_app"): 1,
			key("created_total", catAssigned, "email"):  1,
			key("sent_total", catAssigned, "email"):     1,
		}, counters(t, w))
	})
}

func TestAnEmailInQuietHoursIsHeldThenCancelledWhenReadFirst(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		loc := zagreb(t)
		w := newMailWorld(t, db, notification.TimeZone(loc))
		w.clock.Set(time.Date(2026, 6, 10, 22, 0, 0, 0, loc))
		w.consumeAssigned()
		w.start()

		w.publish(assignedEvent{TicketID: 7, To: []int{1}})
		require.NoError(t, w.drain())

		require.Equal(t, map[string]float64{
			key("created_total", catAssigned, "in_app"): 1,
			key("created_total", catAssigned, "email"):  1,
			key("held_total", catAssigned, "email"):     1,
		}, counters(t, w))

		_, err := w.notices.Inbox.MarkRead(t.Context(), 1, w.inbox(1).Items[0].ID)
		require.NoError(t, err)

		w.clock.Set(time.Date(2026, 6, 11, 7, 0, 0, 0, loc))
		require.Equal(t, 1, w.deliverDue())

		got := counters(t, w)
		require.InDelta(t, 1, got[key("cancelled_total", catAssigned, "email")], 0)
		require.Len(t, got, 4, "no sent, no failed")
	})
}

func TestAnEmailThatFailsCountsItsReason(t *testing.T) {
	for _, c := range []struct {
		name   string
		err    error
		reason string
	}{
		{"permanent", mail.ErrPermanent, "permanent"},
		{"expired", errors.New("mail: cannot reach the relay"), "expired"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
				w := newSenderWorld(t, db, &failing{err: c.err}, notification.TimeZone(zagreb(t)))
				atNoon(w, zagreb(t))
				w.consumeAssigned()
				w.start()

				w.publish(assignedEvent{TicketID: 7, To: []int{1}})
				require.NoError(t, w.drain())

				for range 100 {
					d := w.deliveries()[0]
					if d.Status == "failed" {
						break
					}

					w.clock.Set(d.NextAttemptAt)
					w.deliverDue()
				}

				got := counters(t, w)
				require.InDelta(t, 1, got[key("failed_total", catAssigned, "email", "reason="+c.reason)], 0)
				require.Len(t, got, 3, "created in_app, created email, failed: a retry counts nothing")
			})
		})
	}
}

func TestPushOutcomesCountByCategoryAndChannel(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		status int
		metric string
		extra  []string
	}{
		{http.StatusCreated, "sent_total", nil},
		{http.StatusGone, "failed_total", []string{"reason=gone"}},
		{http.StatusBadRequest, "failed_total", []string{"reason=rejected"}},
	} {
		t.Run(strconv.Itoa(c.status), func(t *testing.T) {
			t.Parallel()

			dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
				t.Helper()

				env := newPushWorld(t, db)
				atNoon(env.world, zagreb(t))
				env.consume()
				env.start()

				env.subscribe(1, env.svc.NewDevice(t))
				env.svc.Answer(c.status)
				env.notify(pushedEvent, 1, 1)
				require.Equal(t, 1, env.deliverPush())

				require.Equal(t, map[string]float64{
					key("created_total", catPushed, "in_app"):    1,
					key("created_total", catPushed, "push"):      1,
					key(c.metric, catPushed, "push", c.extra...): 1,
				}, counters(t, env.world))
			})
		})
	}
}

func TestAPushThatKeepsFailingCountsExpiredOnce(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		env.subscribe(1, env.svc.NewDevice(t))
		env.svc.Answer(http.StatusServiceUnavailable)
		env.notify(urgentEvent, 1, 1)

		for range 40 {
			env.clock.Advance(time.Hour)
			env.deliverPush()
		}

		got := counters(t, env.world)
		require.InDelta(t, 1, got[key("failed_total", "tickets.urgent", "push", "reason=expired")], 0)
		require.Len(t, got, 3)
	})
}

func TestPushesCancelledWithTheirDeviceAndHeldInQuietHoursAreCounted(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		inQuietHours(env)
		env.consume()
		env.start()

		drop := env.subscribe(1, env.svc.NewDevice(t))
		env.notify(pushedEvent, 1, 1)
		env.notify(urgentEvent, 2, 1)

		require.InDelta(t, 2, counters(t, env.world)[key("held_total", "tickets.pushed", "push")]+
			counters(t, env.world)[key("held_total", "tickets.urgent", "push")], 0)

		env.do(1, "DELETE", "/v1/notifications/devices/"+strconv.Itoa(drop.ID), nil)

		got := counters(t, env.world)
		require.InDelta(t, 1, got[key("cancelled_total", "tickets.pushed", "push")], 0)
		require.InDelta(t, 1, got[key("cancelled_total", "tickets.urgent", "push")], 0)
	})
}
