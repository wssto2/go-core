package notification_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/identity/account"
	identitymail "github.com/wssto2/go-core/identity/mailtext"
	"github.com/wssto2/go-core/mail"
	"github.com/wssto2/go-core/notification"
	"github.com/wssto2/go-core/notification/push"
	"github.com/wssto2/go-core/notification/push/pushtest"
	"gorm.io/gorm"
)

// Names the tests use more than once.
const (
	fieldP256DH   = "p256dh"
	fieldAuth     = "auth"
	fieldEndpoint = "endpoint"
	fieldLabel    = "label"
	chanPush      = "push"
	chanEmail     = "email"
	statusSent    = "sent"
)

// The categories of the push tests: pushed by default, pushed by default and urgent, and push only when a person opts
// in.
//
//nolint:gochecknoglobals // categories and events are declared once, as an application does
var (
	pushNormal = notification.Category("tickets.pushed").PushByDefault()
	pushUrgent = notification.Category("tickets.urgent").PushByDefault().HighPriority()
	pushOptIn  = notification.Category("tickets.optin")
	pushBoth   = notification.Category("tickets.both").EmailByDefault().PushByDefault()

	pushedEvent = event.Define[assignedEvent]("tickets.pushed")
	urgentEvent = event.Define[assignedEvent]("tickets.urgent")
	optInEvent  = event.Define[assignedEvent]("tickets.optin")
	bothEvent   = event.Define[assignedEvent]("tickets.both")
)

// pushWorld is a world with web push on, sending to the fake push service svc.
type pushWorld struct {
	*world

	svc *pushtest.Service
}

func newPushWorld(t *testing.T, conn *gorm.DB, opts ...notification.Option) *pushWorld {
	t.Helper()

	svc := pushtest.NewService(t)

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	return &pushWorld{world: buildWorld(t, conn, nil, nil, account.Mail{}, pushOptions(t, svc, opts)...), svc: svc}
}

// newPushMailWorld has e-mail as well as push.
func newPushMailWorld(t *testing.T, conn *gorm.DB, opts ...notification.Option) *pushWorld {
	t.Helper()

	svc, sink := pushtest.NewService(t), mail.NewSink()

	return &pushWorld{
		world: buildWorld(
			t,
			conn,
			nil,
			sink,
			account.Mail{Sender: sink, Renderer: identitymail.Defaults},
			withAppURL(pushOptions(t, svc, opts))...),
		svc: svc,
	}
}

func pushOptions(t *testing.T, svc *pushtest.Service, opts []notification.Option) []notification.Option {
	t.Helper()

	public, private, err := push.GenerateKeys()
	require.NoError(t, err)

	return append([]notification.Option{
		pushNormal, pushUrgent, pushOptIn, pushBoth, notification.TimeZone(zagreb(t)),
		notification.WithPush(
			push.Config{PublicKey: public, PrivateKey: private, Subject: "ops@example.test", Client: svc.Client()},
		),
	}, opts...)
}

// consume collects the consumers of the push categories.
func (w *pushWorld) consume() {
	for _, cat := range []struct {
		ev   event.Event[assignedEvent]
		name string
		kind notification.Kind
	}{
		{pushedEvent, "notifications.pushed", pushNormal}, {urgentEvent, "notifications.urgent", pushUrgent},
		{optInEvent, "notifications.optin", pushOptIn}, {bothEvent, "notifications.both", pushBoth},
	} {
		w.app.Events(cat.ev.To(cat.name, func(ctx context.Context, payload assignedEvent) error {
			return w.notices.Send(ctx, cat.kind, notification.To(payload.To...).Except(payload.Actor),
				func(r notification.Recipient) notification.Message {
					return notification.Message{ //nolint:exhaustruct,exhaustruct_v5 // zero by design
						Title: "Ticket " + strconv.Itoa(payload.TicketID) + " for " + r.Name,
						Body:  "Details",
						Link:  "/tickets/" + strconv.Itoa(payload.TicketID),
					}
				})
		}))
	}
}

func (w *pushWorld) notify(ev event.Event[assignedEvent], ticket int, to ...int) {
	w.t.Helper()
	w.publishEvent(ev, assignedEvent{TicketID: ticket, To: to}) //nolint:exhaustruct,exhaustruct_v5 // zero by design
	require.NoError(w.t, w.drain())
}

// subscribe registers a browser for a person over HTTP and returns the device as the person sees it.
func (w *pushWorld) subscribe(person int, d pushtest.Device) notification.Device {
	w.t.Helper()

	return data[notification.Device](w.t, w.do(person, "POST", "/v1/notifications/devices", map[string]any{
		fieldEndpoint: d.Subscription.Endpoint, fieldP256DH: d.Subscription.P256DH, fieldAuth: d.Subscription.Auth,
	}))
}

func (w *pushWorld) deliverPush() int {
	w.t.Helper()

	n, err := w.notices.DeliverPushDue(w.t.Context())
	require.NoError(w.t, err)

	return n
}

func (w *pushWorld) pushRows() []delivery {
	var out []delivery

	for _, d := range w.deliveries() {
		if d.Channel == chanPush {
			out = append(out, d)
		}
	}

	return out
}

type devicesRow struct {
	ID            uint64
	UserID        uint32
	FailureCount  uint32
	DisabledAt    *time.Time
	LastSuccessAt *time.Time
	LastSeenAt    time.Time
	Label         string
}

func (w *pushWorld) deviceRows() []devicesRow {
	var out []devicesRow
	require.NoError(w.t, w.db.Table("notification_devices").Order("id").Find(&out).Error)

	return out
}

func inQuietHours(w *pushWorld) { w.clock.Set(time.Date(2026, 6, 10, 23, 0, 0, 0, zagreb(w.t))) }

// pushedJSON is what the service worker reads.
type pushedJSON struct {
	NotificationID uint64 `json:"notification_id"` //nolint:tagliatelle // snake_case wire
	Category       string `json:"category"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	Link           string `json:"link"`
	Tag            string `json:"tag"`
	UnreadCount    int    `json:"unread_count"` //nolint:tagliatelle // snake_case wire
}

func payloadOf(t *testing.T, r pushtest.Request) pushedJSON {
	t.Helper()

	var p pushedJSON
	require.NoError(t, json.Unmarshal(r.Payload, &p), string(r.Payload))

	return p
}

// R12: a browser that registers again, as the app does at every start, is one device, refreshed, never a duplicate.
func TestSubscribingTheSameBrowserTwiceIsOneRefreshedDevice(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		atNoon(env.world, zagreb(t))
		env.start()

		phone := env.svc.NewDevice(t)
		first := env.subscribe(1, phone)
		require.True(t, first.Active)
		require.NotEmpty(t, first.EndpointHash)
		require.Equal(t, notification.EndpointHash(phone.Subscription.Endpoint), first.EndpointHash)

		env.clock.Advance(time.Hour)

		second := env.subscribe(1, phone)
		require.Equal(t, first.ID, second.ID, "the same device")
		require.True(t, second.LastSeenAt.After(first.LastSeenAt), "refreshed")
		require.Len(t, env.deviceRows(), 1)

		list := data[notification.DeviceList](t, env.do(1, http.MethodGet, "/v1/notifications/devices", nil))
		require.Len(t, list.Items, 1)
		require.NotContains(
			t,
			env.do(1, http.MethodGet, "/v1/notifications/devices", nil).Body.String(),
			"push/",
			"the endpoint is never sent",
		)
	})
}

func TestARenamedLabelSurvivesTheSamePersonRegisteringAgain(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		env.start()

		phone := env.svc.NewDevice(t)
		dev := env.subscribe(1, phone)

		renamed := data[notification.Device](
			t,
			env.do(
				1,
				"PATCH",
				"/v1/notifications/devices/"+strconv.Itoa(dev.ID),
				map[string]any{fieldLabel: "  Work   phone "},
			),
		)
		require.Equal(t, "Work phone", renamed.Label)
		require.Equal(t, "Work phone", env.subscribe(1, phone).Label)
		require.Equal(
			t,
			http.StatusUnprocessableEntity,
			env.do(1, "PATCH", "/v1/notifications/devices/"+strconv.Itoa(dev.ID), map[string]any{fieldLabel: ""}).Code,
		)
		require.Equal(
			t,
			http.StatusUnprocessableEntity,
			env.do(1, "PATCH", "/v1/notifications/devices/"+strconv.Itoa(dev.ID), map[string]any{fieldLabel: "   "}).Code,
		)
	})
}

// NOTIF-DEVICE-001, rule 2: a shared computer. The endpoint moves to the person who signed in, and the pushes queued
// for the earlier person never reach them.
func TestAnotherPersonRegisteringTheBrowserTakesTheDeviceAndItsQueuedPushes(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		desk := env.svc.NewDevice(t)
		ana := env.subscribe(1, desk)

		env.notify(pushedEvent, 7, 1)
		require.Len(t, env.pushRows(), 1)

		moved := env.subscribe(2, desk)
		require.Equal(t, ana.ID, moved.ID, "one device")
		require.Len(t, env.deviceRows(), 1)
		require.Equal(t, uint32(2), env.deviceRows()[0].UserID)

		require.Empty(t, data[notification.DeviceList](t, env.do(1, http.MethodGet, "/v1/notifications/devices", nil)).Items)
		require.Len(t, data[notification.DeviceList](t, env.do(2, "GET", "/v1/notifications/devices", nil)).Items, 1)

		rows := env.pushRows()
		require.Equal(t, "cancelled", rows[0].Status)
		require.Contains(t, *rows[0].LastError, "moved")

		require.Zero(t, env.deliverPush())
		require.Empty(t, env.svc.Requests(), "Ana's push never reached Ivo")
	})
}

//nolint:funlen // one scenario, read from top to bottom
func TestDevicesBelongToTheirOwnerAndTheRoutesNeedASignedInPerson(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		env.start()

		dev := env.subscribe(1, env.svc.NewDevice(t))
		devID := strconv.Itoa(dev.ID)

		for _, tcase := range []struct{ method, path string }{
			{http.MethodGet, "/v1/notifications/push"}, {http.MethodGet, "/v1/notifications/devices"},
			{http.MethodPost, "/v1/notifications/devices"},
			{"PATCH", "/v1/notifications/devices/" + devID}, {"DELETE", "/v1/notifications/devices/" + devID},
			{http.MethodPost, "/v1/notifications/devices/" + devID + "/test"},
		} {
			require.Equal(t, http.StatusUnauthorized, env.do(0, tcase.method, tcase.path, nil).Code, tcase.method+" "+tcase.path)
		}

		// Ivo cannot see, rename, test or remove Ana's device.
		for _, tcase := range []struct {
			method, path string
			body         any
		}{
			{"PATCH", "/v1/notifications/devices/" + devID, map[string]any{fieldLabel: "mine now"}},
			{"DELETE", "/v1/notifications/devices/" + devID, nil},
			{http.MethodPost, "/v1/notifications/devices/" + devID + "/test", nil},
		} {
			rec := env.do(2, tcase.method, tcase.path, tcase.body)
			require.Equal(t, http.StatusNotFound, rec.Code, tcase.method)
			require.Contains(t, rec.Body.String(), string(notification.ReasonDeviceUnknown))
		}

		require.Len(t, env.deviceRows(), 1)
		require.NotEqual(t, "mine now", env.deviceRows()[0].Label)

		// Signing out removes the person's own endpoint; an endpoint that is someone else's is left alone.
		env.do(2, "POST", "/v1/notifications/devices/unsubscribe", map[string]any{fieldEndpoint: dev.EndpointHash})
		stranger := env.svc.NewDevice(t)
		env.subscribe(1, stranger)
		require.Equal(
			t,
			http.StatusOK,
			env.do(
				2,
				"POST",
				"/v1/notifications/devices/unsubscribe",
				map[string]any{fieldEndpoint: stranger.Subscription.Endpoint},
			).Code,
		)
		require.Len(t, env.deviceRows(), 2, "Ivo's sign-out did not remove Ana's device")

		list := data[notification.DeviceList](
			t,
			env.do(
				1,
				"POST",
				"/v1/notifications/devices/unsubscribe",
				map[string]any{fieldEndpoint: stranger.Subscription.Endpoint},
			),
		)
		require.Len(t, list.Items, 1)
		require.Equal(
			t,
			http.StatusOK,
			env.do(
				1,
				"POST",
				"/v1/notifications/devices/unsubscribe",
				map[string]any{fieldEndpoint: "https://never.registered.test/x"},
			).Code,
		)

		list = data[notification.DeviceList](t, env.do(1, "DELETE", "/v1/notifications/devices/"+devID, nil))
		require.Empty(t, list.Items)
	})
}

func TestASubscriptionThatCannotBePushedToIsRefused(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		env.start()

		good := env.svc.NewDevice(t).Subscription

		for name, body := range map[string]map[string]any{
			"http":     {fieldEndpoint: "http://push.example.test/x", fieldP256DH: good.P256DH, fieldAuth: good.Auth},
			"bad key":  {fieldEndpoint: good.Endpoint, fieldP256DH: "AAAA", fieldAuth: good.Auth},
			"bad auth": {fieldEndpoint: good.Endpoint, fieldP256DH: good.P256DH, fieldAuth: "AAAA"},
		} {
			rec := env.do(1, "POST", "/v1/notifications/devices", body)
			require.Equal(t, http.StatusBadRequest, rec.Code, name)
			require.Contains(t, rec.Body.String(), string(notification.ReasonInvalidSubscription), name)
		}

		require.Equal(
			t,
			http.StatusUnprocessableEntity,
			env.do(1, "POST", "/v1/notifications/devices", map[string]any{fieldEndpoint: good.Endpoint}).Code,
			"missing keys",
		)
		require.Empty(t, env.deviceRows())
	})
}

func TestWithoutPushTheConfigSaysSoAndNothingIsRegistered(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newWorld(t, db, nil).start()

		cfg := data[notification.PushConfig](t, env.do(1, http.MethodGet, "/v1/notifications/push", nil))
		require.False(t, cfg.Enabled)
		require.Empty(t, cfg.PublicKey)

		d := pushtest.NewDevice(t, "https://push.example.test/x").Subscription
		rec := env.do(
			1,
			"POST",
			"/v1/notifications/devices",
			map[string]any{fieldEndpoint: d.Endpoint, fieldP256DH: d.P256DH, fieldAuth: d.Auth},
		)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), string(notification.ReasonPushDisabled))

		prefs := data[notification.Preferences](t, env.do(1, http.MethodGet, "/v1/notifications/preferences", nil))
		require.False(t, prefs.PushAvailable)

		off := env.do(1, "PUT", "/v1/notifications/preferences/tickets.assigned/push", map[string]any{chanPush: true})
		require.Equal(t, http.StatusUnprocessableEntity, off.Code)
		require.Contains(t, off.Body.String(), string(notification.ReasonPushUnavailable))
	})
}

func TestThePushConfigCarriesTheKeyTheBrowserSubscribesWith(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		w := newPushWorld(t, db)
		w.start()

		cfg := data[notification.PushConfig](t, w.do(1, "GET", "/v1/notifications/push", nil))
		require.True(t, cfg.Enabled)
		require.Len(t, cfg.PublicKey, 87, "a 65-byte point in base64url")
	})
}

// NOTIF-DELIVERY-001, rule 2 and G18: the push is encrypted for the device, carries the remaining TTL and an urgency,
// and lives 24 hours, or 4 for a high-priority category.
func TestAPushIsDeliveredEncryptedWithTTLAndUrgencyByPriority(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		phone := env.svc.NewDevice(t)
		dev := env.subscribe(1, phone)

		env.notify(pushedEvent, 7, 1)
		env.notify(urgentEvent, 8, 1)

		rows := env.pushRows()
		require.Len(t, rows, 2)
		require.True(t, rows[0].ExpiresAt.Equal(env.clock.Now().Add(24*time.Hour)), "normal lives a day")
		require.True(t, rows[1].ExpiresAt.Equal(env.clock.Now().Add(4*time.Hour)), "high lives four hours")
		require.Equal(t, uint64(dev.ID), rows[0].DeviceID) //nolint:gosec // a test id
		require.Nil(t, rows[0].Address)

		env.clock.Advance(time.Minute)
		require.Equal(t, 2, env.deliverPush())

		reqs := env.svc.Requests()
		require.Len(t, reqs, 2)

		normal, urgent := payloadOf(t, reqs[0]), payloadOf(t, reqs[1])
		require.Equal(t, "tickets.pushed", normal.Category)
		require.Equal(t, "Ticket 7 for Ana Horvat", normal.Title)
		require.Equal(t, "/tickets/7", normal.Link)
		require.Equal(t, 2, normal.UnreadCount)
		require.Equal(t, "notification-"+strconv.FormatUint(normal.NotificationID, 10), normal.Tag)

		require.Equal(t, "tickets.urgent", urgent.Category)
		require.Equal(t, "normal", reqs[0].Header.Get("Urgency"))
		require.Equal(t, strconv.Itoa(24*3600-60), reqs[0].Header.Get("TTL"), "what is left of the day")
		require.Equal(t, "high", reqs[1].Header.Get("Urgency"))
		require.Equal(t, strconv.Itoa(4*3600-60), reqs[1].Header.Get("TTL"))
		require.Equal(t, "aes128gcm", reqs[0].Header.Get("Content-Encoding"))
		require.True(t, strings.HasPrefix(reqs[0].Header.Get("Authorization"), "vapid t="))

		for _, r := range env.pushRows() {
			require.Equal(t, statusSent, r.Status)
			require.Equal(t, uint32(1), r.Attempts)
		}

		require.NotNil(t, env.deviceRows()[0].LastSuccessAt)
		require.Zero(t, env.deliverPush(), "nothing is sent twice")
		require.Len(t, env.svc.Requests(), 2)
	})
}

// NOTIF-PREF-001 for the push channel: the person's choice over the category's default, the enforcer over both, and
// the person who has no device is simply not pushed to.
//
//nolint:funlen // one scenario, read from top to bottom
func TestPushFollowsEnforcedThenPersonThenDefault(t *testing.T) {
	t.Parallel()

	forcePush := func(_ context.Context, userID int, kind notification.Kind, ch notification.Channel) (bool, bool, error) {
		switch {
		case ch != notification.ChannelPush:
			return false, false, nil
		case kind.Code() == pushOptIn.Code() && userID == 2:
			return true, true, nil // Ivo's dealer wants it
		case kind.Code() == pushNormal.Code() && userID == 1:
			return false, true, nil // Ana's dealer does not
		}

		return false, false, nil
	}

	dbtest.Run(t, func(t *testing.T, conn *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, conn, notification.Enforce(forcePush))
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		for person := 1; person <= 2; person++ {
			env.subscribe(person, env.svc.NewDevice(t))
		}

		// Defaults: pushNormal on, pushOptIn off. Ana is enforced off for pushNormal, Ivo enforced on for pushOptIn.
		env.notify(pushedEvent, 1, 1, 2)
		env.notify(optInEvent, 2, 1, 2)

		got := map[uint32]int{}

		for _, r := range env.pushRows() {
			var dev devicesRow
			require.NoError(t, conn.Table("notification_devices").Where("id = ?", r.DeviceID).Take(&dev).Error)

			got[dev.UserID]++
		}

		require.Equal(t, map[uint32]int{2: 2}, got, "Ivo gets both; Ana's off is enforced and her opt-in is not given")

		// The person's own choice: Ana turns the opt-in category on.
		on := data[notification.CategorySettings](
			t,
			env.do(1, "PUT", "/v1/notifications/preferences/tickets.optin/push", map[string]any{chanPush: true}),
		)
		require.Equal(t, notification.Setting{Enabled: true, Source: notification.SourcePerson}, on.Push)

		env.notify(optInEvent, 3, 1)
		require.Len(t, env.pushRows(), 3)

		// Choosing the default again forgets the choice.
		def := data[notification.CategorySettings](
			t,
			env.do(1, "PUT", "/v1/notifications/preferences/tickets.optin/push", map[string]any{chanPush: false}),
		)
		require.Equal(t, notification.Setting{Enabled: false, Source: notification.SourceDefault}, def.Push)
		require.Zero(t, env.count("notification_preferences"))

		// What is enforced cannot be changed.
		rec := env.do(1, "PUT", "/v1/notifications/preferences/tickets.pushed/push", map[string]any{chanPush: true})
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Contains(t, rec.Body.String(), string(notification.ReasonEnforced))

		// The settings show push per category next to e-mail.
		prefs := data[notification.Preferences](t, env.do(1, http.MethodGet, "/v1/notifications/preferences", nil))
		require.True(t, prefs.PushAvailable)

		byCode := map[string]notification.CategorySettings{}
		for _, c := range prefs.Categories {
			byCode[c.Category] = c
		}

		require.Equal(
			t,
			notification.Setting{Enabled: false, Source: notification.SourceEnforced},
			byCode["tickets.pushed"].Push,
		)
		require.Equal(
			t,
			notification.Setting{Enabled: false, Source: notification.SourceDefault},
			byCode["tickets.optin"].Push,
		)
		require.Equal(
			t,
			notification.Setting{Enabled: false, Source: notification.SourceDefault},
			byCode["tickets.pushed"].Email,
			"the e-mail was not touched",
		)

		// chanPush is required, as chanEmail is.
		require.Equal(
			t,
			http.StatusUnprocessableEntity,
			env.do(2, "PUT", "/v1/notifications/preferences/tickets.optin/push", map[string]any{}).Code,
		)
	})
}

// NOTIF-QUIET-001: a push made in quiet hours is held, due when they end, with its TTL starting there; two held pushes
// are already a bundle.
func TestPushesMadeInQuietHoursWaitAndAreBundledWhenTheyEnd(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		inQuietHours(env)
		env.consume()
		env.start()

		env.subscribe(1, env.svc.NewDevice(t))

		env.notify(pushedEvent, 1, 1)
		env.notify(urgentEvent, 2, 1)
		env.notify(pushedEvent, 3, 1)

		release := time.Date(2026, 6, 11, 7, 0, 0, 0, zagreb(t))
		rows := env.pushRows()
		require.Len(t, rows, 3)

		for _, r := range rows {
			require.Equal(t, "held", r.Status)
			require.True(t, r.NextAttemptAt.Equal(release), "due when quiet hours end")
		}

		require.True(t, rows[1].ExpiresAt.Equal(release.Add(4*time.Hour)), "the TTL starts at the release")

		require.Zero(t, env.deliverPush(), "nothing in the night")

		// The second notification is read in the meantime: it is not pushed.
		page := env.inbox(1)
		require.NoError(
			t,
			env.db.Table("notifications").Where("id = ?", page.Items[1].ID).Update("read_at", env.clock.Now()).Error,
		)

		env.clock.Set(release.Add(time.Second))
		require.Equal(t, 3, env.deliverPush())

		reqs := env.svc.Requests()
		require.Len(t, reqs, 1, "two are left: one bundle")

		bundle := payloadOf(t, reqs[0])
		require.Equal(t, "2 nove obavijesti", bundle.Title, "Ana's language")
		require.Equal(t, "Ticket 1 for Ana Horvat · Ticket 3 for Ana Horvat", bundle.Body)
		require.Equal(t, "/", bundle.Link)
		require.Equal(t, "notification-bundle", bundle.Tag)
		require.Zero(t, bundle.NotificationID)
		require.Equal(t, 2, bundle.UnreadCount)
		require.Equal(t, "normal", reqs[0].Header.Get("Urgency"))

		statuses := make([]string, 0, len(env.pushRows()))
		for _, r := range env.pushRows() {
			statuses = append(statuses, r.Status)
		}

		require.Equal(t, []string{statusSent, "cancelled", statusSent}, statuses)
	})
}

// A burst of three or more pushes due together is one push; fewer go one by one. The bundle carries the highest urgency
// and the shortest TTL of the group.
func TestABurstIsBundledIntoOnePush(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(
			t,
			db,
			notification.PushBundleText(func(locale string, count int, titles []string) (string, string) {
				return strconv.Itoa(count) + " in " + locale, strings.Join(titles, "|")
			}),
		)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		env.subscribe(2, env.svc.NewDevice(t))

		env.notify(pushedEvent, 1, 2)
		env.notify(pushedEvent, 2, 2)
		env.deliverPush()
		require.Len(t, env.svc.Requests(), 2, "two are not a burst")

		env.svc.Reset()
		env.notify(pushedEvent, 3, 2)
		env.notify(urgentEvent, 4, 2)
		env.notify(pushedEvent, 5, 2)
		require.Equal(t, 3, env.deliverPush())

		reqs := env.svc.Requests()
		require.Len(t, reqs, 1)
		require.Equal(t, "3 in en", payloadOf(t, reqs[0]).Title, "the application's text, in Ivo's language")
		require.Equal(t, "high", reqs[0].Header.Get("Urgency"))
		require.Equal(t, strconv.Itoa(4*3600), reqs[0].Header.Get("TTL"), "the shortest of the group")

		for _, r := range env.pushRows() {
			require.Equal(t, statusSent, r.Status)
		}
	})
}

func TestTheDefaultBundleTextIsInThePersonsLanguage(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		env.subscribe(2, env.svc.NewDevice(t))

		for i := range 3 {
			env.notify(pushedEvent, i+1, 2)
		}

		env.deliverPush()
		require.Equal(t, "3 new notifications", payloadOf(t, env.svc.Requests()[0]).Title)
		require.Equal(
			t,
			"Ticket 1 for Ivo · Ticket 2 for Ivo · Ticket 3 for Ivo",
			payloadOf(t, env.svc.Requests()[0]).Body,
		)
	})
}

// NOTIF-DELIVERY-001 and NOTIF-DEVICE-001, rule 5: what each answer of the push service does to the delivery and the
// device.
func TestEachPushServiceAnswerDecidesTheDeliveryAndTheDevice(t *testing.T) {
	t.Parallel()

	retried := map[int]bool{401: true, 403: true, 429: true, 500: true, 502: true, 503: true}

	for status := range retried {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
				t.Helper()

				env := newPushWorld(t, db)
				atNoon(env.world, zagreb(t))
				env.consume()
				env.start()

				env.subscribe(1, env.svc.NewDevice(t))
				env.svc.Answer(status)
				env.notify(pushedEvent, 1, 1)

				now := env.clock.Now()
				require.Equal(t, 1, env.deliverPush())

				row := env.pushRows()[0]
				require.Equal(t, "pending", row.Status, "retried")
				require.Equal(t, uint32(1), row.Attempts)
				require.True(
					t,
					row.NextAttemptAt.Equal(now.Add(30*time.Second)),
					"the first retry waits 30 seconds: %s",
					row.NextAttemptAt,
				)

				dev := env.deviceRows()[0]
				require.Zero(t, dev.FailureCount, "an outage or our credentials do not count against the device")
				require.Nil(t, dev.DisabledAt)

				env.svc.Answer(201)
				env.clock.Advance(time.Minute)
				require.Equal(t, 1, env.deliverPush())
				require.Equal(t, statusSent, env.pushRows()[0].Status)
			})
		})
	}
}

func TestRetryAfterLongerThanTheBackoffIsHonoured(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		env.subscribe(1, env.svc.NewDevice(t))
		env.svc.Answer(http.StatusTooManyRequests)
		env.svc.RetryAfter(600)
		env.notify(pushedEvent, 1, 1)

		now := env.clock.Now()
		env.deliverPush()

		require.True(t, env.pushRows()[0].NextAttemptAt.Equal(now.Add(10*time.Minute)))
	})
}

func TestAPushThatKeepsFailingIsGivenUpOnWhenItsTTLRunsOut(t *testing.T) {
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

		for range 40 { // 30 s doubling to an hour: well past four hours
			env.clock.Advance(time.Hour)
			env.deliverPush()
		}

		row := env.pushRows()[0]
		require.Equal(t, "failed", row.Status)
		require.Contains(t, *row.LastError, "expired")
		require.Nil(t, env.deviceRows()[0].DisabledAt)
	})
}

func TestGoneSubscriptionsRemoveTheDeviceAndCancelWhatWaits(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			dbtest.Run(t, func(t *testing.T, conn *gorm.DB) {
				t.Helper()

				env := newPushWorld(t, conn)
				atNoon(env.world, zagreb(t))
				env.consume()
				env.start()

				env.subscribe(1, env.svc.NewDevice(t))
				env.svc.Answer(status)
				env.notify(pushedEvent, 1, 1)
				env.notify(pushedEvent, 2, 1)

				// The second push is still owed when the first learns the subscription is gone.
				require.NoError(
					t,
					conn.Table("notification_deliveries").Where("notification_id = ?", env.inbox(1).Items[0].ID).
						Update("next_attempt_at", env.clock.Now().Add(time.Hour)).Error,
				)
				require.Equal(t, 1, env.deliverPush())

				require.Empty(t, env.deviceRows(), "removed at once")

				statuses := make([]string, 0, len(env.pushRows()))
				for _, r := range env.pushRows() {
					statuses = append(statuses, r.Status)
				}

				require.ElementsMatch(t, []string{"failed", "cancelled"}, statuses)
			})
		})
	}
}

func TestADeviceThatFailsThroughItsOwnFaultIsDisabledAtTheLimit(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, conn *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, conn)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		env.subscribe(1, env.svc.NewDevice(t))
		env.svc.Answer(http.StatusBadRequest)

		for i := 1; i < notification.MaxConsecutiveFailures; i++ {
			env.notify(pushedEvent, i, 1)
			env.deliverPush()
			require.Equal(t, uint32(i), env.deviceRows()[0].FailureCount)
			require.Nil(t, env.deviceRows()[0].DisabledAt)
		}

		env.svc.Answer(http.StatusCreated)
		env.notify(pushedEvent, 90, 1)
		env.deliverPush()
		require.Zero(t, env.deviceRows()[0].FailureCount, "a success resets the count")

		env.svc.Answer(http.StatusBadRequest)

		for i := 1; i <= notification.MaxConsecutiveFailures; i++ {
			env.notify(pushedEvent, 100+i, 1)

			if i == notification.MaxConsecutiveFailures {
				env.notify(pushedEvent, 200, 1) // owed when the device is disabled
				require.NoError(
					t,
					conn.Table("notification_deliveries").Where("notification_id = ?", env.inbox(1).Items[0].ID).
						Update("next_attempt_at", env.clock.Now().Add(time.Hour)).Error,
				)
			}

			env.deliverPush()
		}

		dev := env.deviceRows()[0]
		require.NotNil(t, dev.DisabledAt)
		require.Equal(t, "cancelled", env.pushRows()[len(env.pushRows())-1].Status, "what was owed is cancelled")

		list := data[notification.DeviceList](t, env.do(1, http.MethodGet, "/v1/notifications/devices", nil))
		require.False(t, list.Items[0].Active)

		before := len(env.pushRows())
		env.notify(pushedEvent, 300, 1)
		require.Len(t, env.pushRows(), before, "a disabled device gets no new push")

		rec := env.do(1, "POST", "/v1/notifications/devices/"+strconv.FormatUint(dev.ID, 10)+"/test", nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), string(notification.ReasonDeviceDisabled))
	})
}

func TestOurPayloadBeingTooLargeIsNotTheDevicesFault(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		env.subscribe(1, env.svc.NewDevice(t))
		env.svc.Answer(http.StatusRequestEntityTooLarge)

		for i := range notification.MaxConsecutiveFailures + 1 {
			env.notify(pushedEvent, i+1, 1)
			env.deliverPush()
		}

		require.Nil(t, env.deviceRows()[0].DisabledAt)
		require.Zero(t, env.deviceRows()[0].FailureCount)
		require.Equal(t, "failed", env.pushRows()[0].Status, "retrying a 413 cannot help")
	})
}

func TestAReadNotificationAndARemovedDeviceAreNotPushed(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		keep := env.subscribe(1, env.svc.NewDevice(t))
		drop := env.subscribe(1, env.svc.NewDevice(t))

		env.notify(pushedEvent, 1, 1)
		require.Len(t, env.pushRows(), 2)

		env.do(1, "DELETE", "/v1/notifications/devices/"+strconv.Itoa(drop.ID), nil)
		env.do(1, "POST", "/v1/notifications/"+strconv.Itoa(env.inbox(1).Items[0].ID)+"/read", nil)

		require.Equal(t, 1, env.deliverPush(), "the removed device's push was cancelled with it")
		require.Empty(t, env.svc.Requests())

		for _, r := range env.pushRows() {
			require.Equal(t, "cancelled", r.Status)
		}

		require.Equal(t, uint64(keep.ID), env.pushRows()[0].DeviceID) //nolint:gosec // a test id
	})
}

// The test route of a device pushes to that device only, at once, through the whole path; the test of the app pushes to
// all.
func TestTheTestNotificationReachesTheDevicesAtOnceWhateverTheSettings(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		inQuietHours(env)
		env.consume()
		env.start()

		phoneA := env.svc.NewDevice(t)
		b := env.svc.NewDevice(t)
		devA := env.subscribe(1, phoneA)
		env.subscribe(1, b)

		rec := env.do(1, "POST", "/v1/notifications/devices/"+strconv.Itoa(devA.ID)+"/test", nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, data[notification.DeviceList](t, rec).Items, 2)
		require.NoError(t, env.drain())

		require.Equal(t, 1, env.deliverPush(), "pending, not held: a test must arrive now")

		reqs := env.svc.Requests()
		require.Len(t, reqs, 1)
		require.Equal(t, "/push/1", reqs[0].Path, "only the device asked about: "+phoneA.Subscription.Endpoint)
		require.Equal(t, "system.test", payloadOf(t, reqs[0]).Category)

		env.svc.Reset()
		require.Equal(t, http.StatusNoContent, env.do(1, "POST", "/v1/notifications/test", nil).Code)
		require.NoError(t, env.drain())
		require.Equal(t, 2, env.deliverPush(), "the test of the app reaches every device")
	})
}

// G16: the e-mail worker and the push worker each claim only their own channel's deliveries, on every database.
func TestTheEmailAndThePushWorkerClaimOnlyTheirOwnChannel(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushMailWorld(t, db)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		env.subscribe(1, env.svc.NewDevice(t))
		env.notify(bothEvent, 1, 1)

		ds := env.deliveries()
		require.Len(t, ds, 2, "one e-mail and one push for the one notification")
		require.ElementsMatch(t, []string{chanEmail, chanPush}, []string{ds[0].Channel, ds[1].Channel})

		require.Equal(t, 1, env.deliverDue(), "the e-mail worker takes the e-mail only")
		require.Len(t, env.sink.Sent(), 1)
		require.Empty(t, env.svc.Requests(), "no push by the e-mail worker")

		for _, d := range env.deliveries() {
			if d.Channel == chanPush {
				require.Equal(t, "pending", d.Status)
				require.Zero(t, d.Attempts)
			}
		}

		require.Equal(t, 1, env.deliverPush(), "the push worker takes the push only")
		require.Len(t, env.svc.Requests(), 1)
		require.Len(t, env.sink.Sent(), 1, "and sends no e-mail")

		for _, d := range env.deliveries() {
			require.Equal(t, statusSent, d.Status, d.Channel)
			require.Equal(t, uint32(1), d.Attempts, d.Channel)
		}
	})
}

// A push goes out once however many instances run the worker.
func TestPushWorkersRacingOnTheSameDeliveriesSendEachOnce(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		for person := 1; person <= 2; person++ {
			env.subscribe(person, env.svc.NewDevice(t))
		}

		for i := range 20 {
			env.notify(pushedEvent, i+1, 1, 2)
		}

		var group sync.WaitGroup

		for range 4 {
			group.Go(func() {
				_, err := env.notices.DeliverPushDue(context.Background())
				if err != nil && !strings.Contains(err.Error(), "Deadlock") {
					t.Errorf("DeliverPushDue: %v", err)
				}
			})
		}

		group.Wait()

		for left := 1; left > 0; { // what a deadlocked claim left
			left = env.deliverPush()
		}

		// 20 notifications per person, bundled however the batches fell: every notification went out once.
		for _, r := range env.pushRows() {
			require.Equal(t, statusSent, r.Status)
			require.Equal(t, uint32(1), r.Attempts)
		}
	})
}

func TestAPersonWithoutADeviceOrWithoutPushOnIsNotPushedTo(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		t.Helper()

		env := newPushWorld(t, db)
		atNoon(env.world, zagreb(t))
		env.consume()
		env.start()

		env.notify(pushedEvent, 1, 1, 2)
		require.Empty(t, env.pushRows(), "nobody has a device")
		require.Len(t, env.inbox(1).Items, 1, "the inbox is always on")
	})
}
