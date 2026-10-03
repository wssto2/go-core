package notification_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/identity/account"
	identitymail "github.com/wssto2/go-core/identity/mailtext"
	"github.com/wssto2/go-core/notification"
	"gorm.io/gorm"
)

// forceAssigned enforces the e-mail of tickets.assigned on for person 1 and off for person 2.
func forceAssigned(_ context.Context, userID int, k notification.Kind) (on, enforced bool, err error) {
	if k.Code() != TicketAssigned.Code() {
		return false, false, nil
	}

	switch userID {
	case 1:
		return true, true, nil
	case 2:
		return false, true, nil
	}

	return false, false, nil
}

func settingsOf(t *testing.T, w *world, person int) map[string]notification.Setting {
	t.Helper()

	prefs, err := w.notices.Settings.Get(t.Context(), person)
	require.NoError(t, err)

	out := map[string]notification.Setting{}
	for _, c := range prefs.Categories {
		out[c.Category] = c.Email
	}

	return out
}

// NOTIF-PREF-001: the enforced setting, then the person's own choice, then the category's default.
func TestAnEnforcedSettingBeatsThePersonsChoiceWhichBeatsTheDefault(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db, notification.Enforce(forceAssigned))
		settings := w.notices.Settings

		// Eva (3): the defaults. tickets.assigned is e-mailed by default, tickets.commented is not.
		require.Equal(t, map[string]notification.Setting{
			"tickets.assigned":  {Enabled: true, Source: notification.SourceDefault},
			"tickets.commented": {Enabled: false, Source: notification.SourceDefault},
		}, settingsOf(t, w, 3))

		// Her own choices beat the defaults, both ways.
		_, err := settings.SetEmail(t.Context(), 3, "tickets.assigned", false)
		require.NoError(t, err)
		_, err = settings.SetEmail(t.Context(), 3, "tickets.commented", true)
		require.NoError(t, err)
		require.Equal(t, map[string]notification.Setting{
			"tickets.assigned":  {Enabled: false, Source: notification.SourcePerson},
			"tickets.commented": {Enabled: true, Source: notification.SourcePerson},
		}, settingsOf(t, w, 3))

		// Ana (1) has it enforced on, Ivo (2) enforced off: whatever they chose before, and the other category is theirs.
		require.Equal(t, notification.Setting{Enabled: true, Source: notification.SourceEnforced}, settingsOf(t, w, 1)["tickets.assigned"])
		require.Equal(t, notification.Setting{Enabled: false, Source: notification.SourceEnforced}, settingsOf(t, w, 2)["tickets.assigned"])
		require.Equal(t, notification.SourceDefault, settingsOf(t, w, 1)["tickets.commented"].Source)

		_, err = settings.SetEmail(t.Context(), 2, "tickets.commented", true)
		require.NoError(t, err)

		// A choice stored before the enforcement is kept underneath it, and loses to it.
		require.NoError(t, db.Table("notification_preferences").Create(map[string]any{"user_id": 2, "category": "tickets.assigned", "channel": "email", "enabled": true, "updated_at": time.Now()}).Error)
		require.False(t, settingsOf(t, w, 2)["tickets.assigned"].Enabled, "the enforced setting wins over the stored choice")
	})
}

func TestAnEnforcedSettingCannotBeChanged(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db, notification.Enforce(forceAssigned))

		_, err := w.notices.Settings.SetEmail(t.Context(), 1, "tickets.assigned", false)
		require.True(t, apperr.HasReason(err, notification.ReasonEnforced))
		require.Equal(t, 422, apperr.GetHTTPStatus(err))
		require.Equal(t, int64(0), w.count("notification_preferences"), "nothing was stored")
	})
}

func TestTheEnforcersErrorIsAnErrorNotAGuess(t *testing.T) {
	w := newMailWorld(t, mustSQLite(t), notification.Enforce(func(context.Context, int, notification.Kind) (bool, bool, error) {
		return false, false, errors.New("the policy store is away")
	}))

	_, err := w.notices.Settings.Get(t.Context(), 1)
	require.ErrorContains(t, err, "the policy store is away")
}

// Only a choice that differs from the category's default is stored: a person who never touched a setting follows the default.
func TestOnlyAChoiceThatDiffersFromTheDefaultIsStored(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db)
		settings := w.notices.Settings

		got, err := settings.SetEmail(t.Context(), 1, "tickets.assigned", true) // the default
		require.NoError(t, err)
		require.Equal(t, notification.Setting{Enabled: true, Source: notification.SourceDefault}, got.Email)
		require.Equal(t, int64(0), w.count("notification_preferences"))

		got, err = settings.SetEmail(t.Context(), 1, "tickets.assigned", false)
		require.NoError(t, err)
		require.Equal(t, notification.Setting{Enabled: false, Source: notification.SourcePerson}, got.Email)
		require.Equal(t, int64(1), w.count("notification_preferences"))

		_, err = settings.SetEmail(t.Context(), 1, "tickets.assigned", false) // again: still one row
		require.NoError(t, err)
		require.Equal(t, int64(1), w.count("notification_preferences"))

		_, err = settings.SetEmail(t.Context(), 1, "tickets.assigned", true) // back to the default: the row goes
		require.NoError(t, err)
		require.Equal(t, int64(0), w.count("notification_preferences"))

		// Ana's choice is hers: Ivo still follows the default.
		_, err = settings.SetEmail(t.Context(), 1, "tickets.commented", true)
		require.NoError(t, err)
		require.Equal(t, notification.SourceDefault, settingsOf(t, w, 2)["tickets.commented"].Source)
	})
}

func TestOnlyTheApplicationsCategoriesAreListedAndConfigurable(t *testing.T) {
	w := newMailWorld(t, mustSQLite(t))

	prefs, err := w.notices.Settings.Get(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, prefs.EmailAvailable)
	require.Equal(t, []string{"tickets.assigned", "tickets.commented"}, []string{prefs.Categories[0].Category, prefs.Categories[1].Category}, "in the order registered; system.test is internal")
	require.Len(t, prefs.Categories, 2)

	for _, code := range []string{"system.test", "tickets.forgotten"} {
		_, err = w.notices.Settings.SetEmail(t.Context(), 1, code, true)
		require.True(t, apperr.HasReason(err, notification.ReasonCategoryUnknown), code)
		require.Equal(t, 404, apperr.GetHTTPStatus(err), code)
	}
}

// Without mail (identity.WithoutMail) e-mail is unavailable: the settings say so, all read off and none can be turned on.
func TestWithoutMailEmailIsUnavailableAndSaysSo(t *testing.T) {
	w := newWorld(t, mustSQLite(t), nil)

	prefs, err := w.notices.Settings.Get(t.Context(), 1)
	require.NoError(t, err)
	require.False(t, prefs.EmailAvailable)

	for _, c := range prefs.Categories {
		require.Equal(t, notification.Setting{Enabled: false, Source: notification.SourceDefault}, c.Email, c.Category)
	}

	_, err = w.notices.Settings.SetEmail(t.Context(), 1, "tickets.commented", true)
	require.True(t, apperr.HasReason(err, notification.ReasonEmailUnavailable))
	require.Equal(t, 422, apperr.GetHTTPStatus(err))
}

func TestMailNeedsTheApplicationsAddressAndCheckSaysSo(t *testing.T) {
	w := newMailWorld(t, mustSQLite(t))
	w2 := buildWorld(t, mustSQLite(t), nil, w.sink, account.Mail{Sender: w.sink, Renderer: identitymail.Defaults}) // mail, but no AppURL

	_, err := w2.app.Handler()
	require.ErrorContains(t, err, "e-mail links need notification.AppURL(")
	require.ErrorContains(t, err, "Fix:")

	_, err = w.app.Handler()
	require.NoError(t, err)

	// No mail: no address needed.
	_, err = newWorld(t, mustSQLite(t), nil).app.Handler()
	require.NoError(t, err)
}

// NOTIF-QUIET-001: on, 21:00 to 07:00 until the person changes it; invalid hours are refused.
func TestQuietHoursAreTheDefaultUntilThePersonSetsTheirOwn(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db)
		settings := w.notices.Settings

		prefs, err := settings.Get(t.Context(), 1)
		require.NoError(t, err)
		require.Equal(t, notification.QuietHours{Enabled: true, Start: 21 * 60, End: 7 * 60}, prefs.QuietHours)
		require.Equal(t, time.Local.String(), prefs.TimeZone)

		mine := notification.QuietHours{Enabled: true, Start: 22*60 + 30, End: 6*60 + 30}
		saved, err := settings.SetQuietHours(t.Context(), 1, mine)
		require.NoError(t, err)
		require.Equal(t, mine, saved)

		off := notification.QuietHours{Enabled: false, Start: 21 * 60, End: 7 * 60}
		_, err = settings.SetQuietHours(t.Context(), 1, mine) // twice: one row
		require.NoError(t, err)
		require.Equal(t, int64(1), w.count("notification_quiet_hours"))

		prefs, _ = settings.Get(t.Context(), 1)
		require.Equal(t, mine, prefs.QuietHours)

		_, err = settings.SetQuietHours(t.Context(), 1, off)
		require.NoError(t, err)

		prefs, _ = settings.Get(t.Context(), 1)
		require.Equal(t, off, prefs.QuietHours)

		other, _ := settings.Get(t.Context(), 2)
		require.Equal(t, notification.DefaultQuietHours(), other.QuietHours, "Ivo has his own")
	})
}

func TestInvalidQuietHoursAreRefused(t *testing.T) {
	w := newMailWorld(t, mustSQLite(t))

	for name, q := range map[string]notification.QuietHours{
		"empty":         {Enabled: true, Start: 60, End: 60},
		"negative":      {Enabled: true, Start: -1, End: 60},
		"a whole day":   {Enabled: true, Start: 60, End: 1440},
		"disabled, too": {Enabled: false, Start: 70, End: 70},
	} {
		_, err := w.notices.Settings.SetQuietHours(t.Context(), 1, q)
		require.True(t, apperr.HasReason(err, notification.ReasonQuietHoursInvalid), name)
		require.Equal(t, 422, apperr.GetHTTPStatus(err), name)
	}

	require.Equal(t, int64(0), w.count("notification_quiet_hours"))
}

// ReleaseAt: now outside quiet hours, the end of the window inside them, read on the wall clock of the zone.
func TestQuietHoursReleaseAt(t *testing.T) {
	zagreb, err := time.LoadLocation("Europe/Zagreb")
	require.NoError(t, err)

	quiet := notification.DefaultQuietHours()
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 9, day, hour, minute, 0, 0, zagreb) }

	require.Equal(t, at(14, 12, 0), quiet.ReleaseAt(at(14, 12, 0), zagreb), "daytime goes out now")
	require.Equal(t, at(15, 7, 0), quiet.ReleaseAt(at(14, 21, 0), zagreb), "21:00 is already quiet")
	require.Equal(t, at(15, 7, 0), quiet.ReleaseAt(at(14, 23, 59), zagreb), "across midnight")
	require.Equal(t, at(15, 7, 0), quiet.ReleaseAt(at(15, 3, 30), zagreb), "after midnight")
	require.Equal(t, at(15, 7, 0), quiet.ReleaseAt(at(15, 7, 0), zagreb), "07:00 is no longer quiet")
	require.Equal(t, at(14, 20, 59), quiet.ReleaseAt(at(14, 20, 59), zagreb), "20:59 is not yet quiet")

	// The server may run in UTC; the wall clock of the zone decides.
	require.True(t, at(15, 7, 0).Equal(quiet.ReleaseAt(at(14, 22, 0).UTC(), zagreb)))

	daytime := notification.QuietHours{Enabled: true, Start: 13 * 60, End: 14 * 60}
	require.Equal(t, at(14, 14, 0), daytime.ReleaseAt(at(14, 13, 15), zagreb))

	off := notification.QuietHours{Enabled: false, Start: 0, End: 23 * 60}
	require.Equal(t, at(14, 3, 0), off.ReleaseAt(at(14, 3, 0), zagreb))

	invalid := notification.QuietHours{Enabled: true, Start: 60, End: 60}
	require.Equal(t, at(14, 3, 0), invalid.ReleaseAt(at(14, 3, 0), zagreb), "invalid hours hold nothing")
}

// Both daylight-saving changes of 2026 in Zagreb: the night ends at 07:00 on the wall clock, however long it was.
func TestQuietHoursEndAtTheChosenHourOnBothDaylightSavingNights(t *testing.T) {
	zagreb, err := time.LoadLocation("Europe/Zagreb")
	require.NoError(t, err)

	quiet := notification.DefaultQuietHours()

	// Clocks go forward on 29 March 2026 at 02:00: the night is 8 hours long.
	spring := quiet.ReleaseAt(time.Date(2026, 3, 28, 22, 0, 0, 0, zagreb), zagreb)
	require.Equal(t, time.Date(2026, 3, 29, 7, 0, 0, 0, zagreb), spring)
	require.Equal(t, 8*time.Hour, spring.Sub(time.Date(2026, 3, 28, 22, 0, 0, 0, zagreb)))

	// Created in the hour after the change, still in the same night.
	require.Equal(t, spring, quiet.ReleaseAt(time.Date(2026, 3, 29, 3, 30, 0, 0, zagreb), zagreb))

	// Clocks go back on 25 October 2026 at 03:00: the night is 10 hours long.
	autumn := quiet.ReleaseAt(time.Date(2026, 10, 24, 22, 0, 0, 0, zagreb), zagreb)
	require.Equal(t, time.Date(2026, 10, 25, 7, 0, 0, 0, zagreb), autumn)
	require.Equal(t, 10*time.Hour, autumn.Sub(time.Date(2026, 10, 24, 22, 0, 0, 0, zagreb)))

	// Created in the repeated hour (02:30 happens twice): both release at 07:00.
	first := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC) // 02:30 CEST
	second := first.Add(time.Hour)                          // 02:30 CET
	require.Equal(t, autumn, quiet.ReleaseAt(first, zagreb))
	require.Equal(t, autumn, quiet.ReleaseAt(second, zagreb))

	// A window that ends in the skipped hour (02:30 does not exist on 29 March) releases after the gap, never before now.
	odd := notification.QuietHours{Enabled: true, Start: 22 * 60, End: 2*60 + 30}
	now := time.Date(2026, 3, 28, 23, 0, 0, 0, zagreb)
	release := odd.ReleaseAt(now, zagreb)
	require.True(t, release.After(now))
	require.LessOrEqual(t, release.Sub(now), 4*time.Hour)
}
