package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/gocore"
)

// Source says which step of NOTIF-PREF-001 decided a setting: the outside authority given to Enforce, the person's own
// choice, or the category's default, in that order of precedence.
type Source string

// The three sources of a setting.
const (
	SourceEnforced Source = "enforced"
	SourcePerson   Source = "person"
	SourceDefault  Source = "default"
)

// Setting is the resolved e-mail setting of one category for one person.
type Setting struct {
	// Enabled is whether the e-mail of this category is sent to the person.
	Enabled bool `json:"enabled"`
	// Source is who decided: "enforced" (the person cannot change it), "person" or "default".
	Source Source `json:"source"`
}

// Enforced reports whether the person cannot change the setting.
func (s Setting) Enforced() bool { return s.Source == SourceEnforced }

// CategorySettings is one category with the person's settings for it. Only e-mail is configurable: the in-app
// notification is always on.
type CategorySettings struct {
	Category string  `json:"category"`
	Email    Setting `json:"email"`
}

// Preferences is a person's notification settings: what the settings screen shows.
type Preferences struct {
	// EmailAvailable is false when the application cannot send e-mail (identity runs WithoutMail): every
	// setting then reads off and cannot be turned on, and no e-mail is ever made.
	EmailAvailable bool `json:"email_available"`
	// Categories are the application's categories a person may configure, in the order they were registered:
	// the module's internal test category is not among them.
	Categories []CategorySettings `json:"categories"`
	// QuietHours are the person's own, or the default for everyone until they change them.
	QuietHours QuietHours `json:"quiet_hours"`
	// TimeZone is the zone quiet hours are read in, such as "Europe/Zagreb".
	TimeZone string `json:"time_zone"`
}

// The reasons of the errors a person's settings can answer with; the client translates them.
const (
	// ReasonEnforced: the setting is enforced and the person cannot change it (422).
	ReasonEnforced apperr.Reason = "notification.setting.enforced"
	// ReasonEmailUnavailable: the application sends no e-mail, so there is nothing to turn on (422).
	ReasonEmailUnavailable apperr.Reason = "notification.email.unavailable"
	// ReasonQuietHoursInvalid: the quiet hours are out of range, or start where they end (422).
	ReasonQuietHoursInvalid apperr.Reason = "notification.quiet_hours.invalid"
	// ReasonCategoryUnknown: there is no such category to configure (404).
	ReasonCategoryUnknown apperr.Reason = "notification.category.unknown"
)

// emailChannel is the channel column's value of a person's e-mail choice.
const emailChannel = "email"

// Settings is what a person does with their own notification settings: see them, choose whether a category is
// e-mailed, and set their quiet hours. Every method acts on one person, whose id the caller takes from the
// session, never from the request. Precedence is NOTIF-PREF-001: Enforce, then the person's choice, then the
// category's default; only a choice that differs from the default is stored, so a person who never touched
// a setting follows the default if it changes.
type Settings struct {
	store     *store
	kinds     []Kind
	enforce   Enforcer
	available bool
	clock     gocore.Clock
	location  *time.Location
}

// Get returns the person's settings (NOTIF-PREF-001, NOTIF-QUIET-001).
func (s *Settings) Get(ctx context.Context, userID int) (Preferences, error) {
	chosen, err := s.store.choicesOf(ctx, userID)
	if err != nil {
		return Preferences{}, apperr.Internal(err)
	}

	quiet, err := s.quietOf(ctx, userID)
	if err != nil {
		return Preferences{}, err
	}

	out := Preferences{EmailAvailable: s.available, QuietHours: quiet, TimeZone: s.location.String()}

	for _, k := range s.kinds {
		if k.internal {
			continue
		}

		setting, err := s.resolve(ctx, k, userID, chosen)
		if err != nil {
			return Preferences{}, err
		}

		out.Categories = append(out.Categories, CategorySettings{Category: k.code, Email: setting})
	}

	return out, nil
}

// SetEmail turns the e-mail of the category on or off for the person and returns the setting as it is now. A setting the
// application enforces is refused (ReasonEnforced), and so is turning e-mail on when the application sends none
// (ReasonEmailUnavailable). An unknown or internal category is not found. Only a choice that differs from the
// category's default is kept.
func (s *Settings) SetEmail(ctx context.Context, userID int, category string, on bool) (CategorySettings, error) {
	k, ok := s.configurable(category)
	if !ok {
		return CategorySettings{}, apperr.NotFound("notification category not found").WithReason(ReasonCategoryUnknown)
	}

	if !s.available {
		return CategorySettings{}, apperr.New(nil, string(ReasonEmailUnavailable), apperr.CodeInvalidArgument).WithReason(ReasonEmailUnavailable)
	}

	current, err := s.resolve(ctx, k, userID, map[string]bool{}) // only the enforcer matters here
	if err != nil {
		return CategorySettings{}, err
	}

	if current.Enforced() {
		return CategorySettings{}, apperr.New(nil, string(ReasonEnforced), apperr.CodeInvalidArgument).WithReason(ReasonEnforced)
	}

	if on == k.email {
		err = s.store.clearChoice(ctx, userID, k.code, emailChannel)
	} else {
		err = s.store.setChoice(ctx, userID, k.code, emailChannel, on, whole(s.clock.Now()))
	}

	if err != nil {
		return CategorySettings{}, apperr.Internal(err)
	}

	setting := Setting{Enabled: on, Source: SourcePerson}
	if on == k.email {
		setting.Source = SourceDefault
	}

	return CategorySettings{Category: k.code, Email: setting}, nil
}

// SetQuietHours sets the person's quiet hours and returns them. Invalid ones (out of range, or start equal to end)
// are refused with ReasonQuietHoursInvalid.
func (s *Settings) SetQuietHours(ctx context.Context, userID int, q QuietHours) (QuietHours, error) {
	if q.Validate() != nil {
		return QuietHours{}, apperr.New(ErrInvalidQuietHours, string(ReasonQuietHoursInvalid), apperr.CodeValidationError).WithReason(ReasonQuietHoursInvalid)
	}

	if err := s.store.saveQuiet(ctx, userID, q, whole(s.clock.Now())); err != nil {
		return QuietHours{}, apperr.Internal(err)
	}

	return q, nil
}

func (s *Settings) configurable(code string) (Kind, bool) {
	for _, k := range s.kinds {
		if k.code == code && !k.internal {
			return k, true
		}
	}

	return Kind{}, false
}

func (s *Settings) quietOf(ctx context.Context, userID int) (QuietHours, error) {
	found, err := s.store.quietOf(ctx, []int{userID})
	if err != nil {
		return QuietHours{}, apperr.Internal(err)
	}

	if q, ok := found[userID]; ok {
		return q, nil
	}

	return DefaultQuietHours(), nil
}

// resolve is NOTIF-PREF-001 for one person and one category: the enforced setting, else the person's choice (chosen,
// read from the store when nil), else the default. Nothing is e-mailed without mail, and an internal category is
// never e-mailed.
func (s *Settings) resolve(ctx context.Context, k Kind, userID int, chosen map[string]bool) (Setting, error) {
	if !s.available || k.internal {
		return Setting{Enabled: false, Source: SourceDefault}, nil
	}

	if s.enforce != nil {
		on, enforced, err := s.enforce(ctx, userID, k)
		if err != nil {
			return Setting{}, apperr.Internal(fmt.Errorf("notification: the enforcer failed: %w", err))
		}

		if enforced {
			return Setting{Enabled: on, Source: SourceEnforced}, nil
		}
	}

	if chosen == nil {
		var err error
		if chosen, err = s.store.choicesOf(ctx, userID); err != nil {
			return Setting{}, apperr.Internal(err)
		}
	}

	if on, ok := chosen[k.code]; ok {
		return Setting{Enabled: on, Source: SourcePerson}, nil
	}

	return Setting{Enabled: k.email, Source: SourceDefault}, nil
}

// emailFor resolves the e-mail setting of one category for several people, with one read of their choices and
// one of their quiet hours, for Send.
func (s *Settings) emailFor(ctx context.Context, k Kind, ids []int) (map[int]Setting, error) {
	out := make(map[int]Setting, len(ids))

	if !s.available || k.internal {
		return out, nil
	}

	chosen, err := s.store.choicesFor(ctx, ids, k.code, emailChannel)
	if err != nil {
		return nil, err
	}

	for _, id := range ids {
		var mine map[string]bool
		if on, ok := chosen[id]; ok {
			mine = map[string]bool{k.code: on}
		} else {
			mine = map[string]bool{}
		}

		setting, err := s.resolve(ctx, k, id, mine)
		if err != nil {
			return nil, err
		}

		out[id] = setting
	}

	return out, nil
}
