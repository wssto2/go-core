package notification_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/notification"
	"gorm.io/gorm"
)

// The settings routes need a signed-in person and no permission, and act on that person's own settings.
func TestThePreferencesRoutesServeOnlyThePersonsOwnSettings(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db, notification.Enforce(forceAssigned))
		w.start()

		for _, tc := range []struct{ method, path string }{
			{"GET", "/v1/notifications/preferences"},
			{"PUT", "/v1/notifications/preferences/tickets.commented"},
			{"PUT", "/v1/notifications/quiet-hours"},
		} {
			require.Equal(t, http.StatusUnauthorized, w.do(0, tc.method, tc.path, nil).Code, tc.method+" "+tc.path)
		}

		prefs := data[notification.Preferences](t, w.do(3, "GET", "/v1/notifications/preferences", nil))
		require.True(t, prefs.EmailAvailable)
		require.Equal(t, []notification.CategorySettings{
			{Category: "tickets.assigned", Email: notification.Setting{Enabled: true, Source: notification.SourceDefault}},
			{Category: "tickets.commented", Email: notification.Setting{Enabled: false, Source: notification.SourceDefault}},
		}, prefs.Categories)
		require.Equal(t, notification.DefaultQuietHours(), prefs.QuietHours)

		// Eva's choice is hers.
		set := data[notification.CategorySettings](t, w.do(3, "PUT", "/v1/notifications/preferences/tickets.commented", map[string]any{"email": true}))
		require.Equal(t, notification.CategorySettings{Category: "tickets.commented", Email: notification.Setting{Enabled: true, Source: notification.SourcePerson}}, set)

		prefs = data[notification.Preferences](t, w.do(3, "GET", "/v1/notifications/preferences", nil))
		require.True(t, prefs.Categories[1].Email.Enabled)

		other := data[notification.Preferences](t, w.do(2, "GET", "/v1/notifications/preferences", nil))
		require.False(t, other.Categories[1].Email.Enabled, "Ivo did not change it")

		// Back to the default: nothing is stored.
		data[notification.CategorySettings](t, w.do(3, "PUT", "/v1/notifications/preferences/tickets.commented", map[string]any{"email": false}))
		require.Zero(t, w.count("notification_preferences"))

		quiet := data[notification.QuietHours](t, w.do(3, "PUT", "/v1/notifications/quiet-hours", notification.QuietHoursInput{Enabled: true, Start: 22 * 60, End: 6 * 60}))
		require.Equal(t, notification.QuietHours{Enabled: true, Start: 1320, End: 360}, quiet)

		prefs = data[notification.Preferences](t, w.do(3, "GET", "/v1/notifications/preferences", nil))
		require.Equal(t, quiet, prefs.QuietHours)

		require.Equal(t, notification.DefaultQuietHours(), data[notification.Preferences](t, w.do(1, "GET", "/v1/notifications/preferences", nil)).QuietHours)
	})
}

// An enforced setting answers 422 with a code the client translates, and changes nothing.
func TestAnEnforcedSettingAnswers422WithItsCode(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db, notification.Enforce(forceAssigned)).start()

		prefs := data[notification.Preferences](t, w.do(1, "GET", "/v1/notifications/preferences", nil))
		require.Equal(t, notification.Setting{Enabled: true, Source: notification.SourceEnforced}, prefs.Categories[0].Email)

		rec := w.do(1, "PUT", "/v1/notifications/preferences/tickets.assigned", map[string]any{"email": false})
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Contains(t, rec.Body.String(), string(notification.ReasonEnforced))
		require.Zero(t, w.count("notification_preferences"))
	})
}

func TestTheSettingsRoutesRefuseWhatCannotBeSet(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db).start()

		unknown := w.do(1, "PUT", "/v1/notifications/preferences/system.test", map[string]any{"email": true})
		require.Equal(t, http.StatusNotFound, unknown.Code, "the module's own category is not configurable")
		require.Contains(t, unknown.Body.String(), string(notification.ReasonCategoryUnknown))

		require.Equal(t, http.StatusNotFound, w.do(1, "PUT", "/v1/notifications/preferences/tickets.forgotten", map[string]any{"email": true}).Code)
		require.Equal(t, http.StatusUnprocessableEntity, w.do(1, "PUT", "/v1/notifications/preferences/tickets.commented", map[string]any{}).Code, "email is required: not read as off")

		for name, q := range map[string]map[string]any{
			"empty":        {"enabled": true, "start": 60, "end": 60},
			"out of range": {"enabled": true, "start": 60, "end": 1440},
			"negative":     {"enabled": true, "start": -5, "end": 60},
		} {
			rec := w.do(1, "PUT", "/v1/notifications/quiet-hours", q)
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		}

		require.Contains(t, w.do(1, "PUT", "/v1/notifications/quiet-hours", map[string]any{"enabled": true, "start": 60, "end": 60}).Body.String(), string(notification.ReasonQuietHoursInvalid))
		require.Zero(t, w.count("notification_quiet_hours"))
	})
}

// Without mail the settings say e-mail is unavailable, and turning it on is refused with its code.
func TestWithoutMailThePreferencesSayEmailIsUnavailable(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, authztest.AllowAll()).start()

		prefs := data[notification.Preferences](t, w.do(1, "GET", "/v1/notifications/preferences", nil))
		require.False(t, prefs.EmailAvailable)
		require.Len(t, prefs.Categories, 2)

		rec := w.do(1, "PUT", "/v1/notifications/preferences/tickets.commented", map[string]any{"email": true})
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Contains(t, rec.Body.String(), string(notification.ReasonEmailUnavailable))

		// Quiet hours can still be set: they will apply if e-mail ever comes.
		require.Equal(t, http.StatusOK, w.do(1, "PUT", "/v1/notifications/quiet-hours", notification.DefaultQuietHours()).Code)
	})
}

// GET /v1/events/consumers: the names of the application's consumers, sorted, for the dead letters' filter; behind the view permission.
func TestTheConsumersRouteListsTheApplicationsConsumersForTheFilter(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, authztest.DenyAll().Allow(notification.ViewDeadLetters))
		w.consumeAssigned()
		w.failingConsumer("billing.invoice-mail")
		w.start()

		got := data[notification.ConsumerNames](t, w.do(1, "GET", "/v1/events/consumers", nil))
		require.Equal(t, []string{"billing.invoice-mail", "notification.test", "notifications.ticket-assigned"}, got.Consumers, "a feature's consumers, added after Install, are in it")

		denied := newWorld(t, db, authztest.DenyAll()).start()
		require.Equal(t, http.StatusForbidden, denied.do(1, "GET", "/v1/events/consumers", nil).Code)
	})
}
