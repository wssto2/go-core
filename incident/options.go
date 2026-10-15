package incident

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/notification"
	"github.com/wssto2/go-core/ratelimit"
)

// Option is what Install takes after the people: Capture, Alerts, or one of the functions ClientErrors, Retain and Link.
// They go in one list, in any order, each at most once.
type Option interface{ apply(*config) }

type optionFunc func(*config)

func (f optionFunc) apply(c *config) { f(c) }

// The defaults of the options.
const (
	// DefaultTrailSize is how many API calls of a person the trail remembers (INC-TRAIL-001).
	DefaultTrailSize = 30
	// DefaultCooldown is how long an immediate alert of a severity holds back the next one of the same severity.
	DefaultCooldown = 5 * time.Minute
	// DefaultDigestEvery is how often the incidents held back or below the immediate severities are sent as one alert.
	DefaultDigestEvery = 30 * time.Minute
	// DefaultOccurrenceRetention is how long the occurrences of an incident are kept, except the newest.
	DefaultOccurrenceRetention = 90 * 24 * time.Hour
	// DefaultClientErrorLimit is how many browser errors one person may report per minute.
	DefaultClientErrorLimit = 20
)

// Capture configures the capture of server errors, which Install puts before every route: a response of 500 or more
// from the errors the request collected, and a panic, which is recorded and re-raised so the answer does not change.
// The zero Capture is the default of every field. Pass one to Install.
type Capture struct {
	// TrailSize is how many API calls of each person are remembered, to be recorded with an incident of their next
	// failing call (INC-TRAIL-001). Zero is DefaultTrailSize; a negative number turns the trail off.
	TrailSize int
	// Environment is what an occurrence says it happened in, such as "production"; the default is the application's
	// environment (APP_ENV), else "unknown".
	Environment string
	// Module names the part of the application a request path belongs to, such as "vehicles" for
	// "/v1/vehicles/12" (the path without the application's prefix). Incidents are grouped by it. The default is
	// the first segment after the version ("v1").
	Module func(path string) string
}

func (c Capture) apply(cfg *config) { cfg.capture = c }

// Severity is an incident's impact. The constants are the alert severities of Alerts.Immediate.
type Severity = core.Severity

// The severities.
const (
	SeverityLow      = core.SeverityLow
	SeverityMedium   = core.SeverityMedium
	SeverityHigh     = core.SeverityHigh
	SeverityCritical = core.SeverityCritical
)

// Holders names the people who hold a permission: the people an alert goes to. The default reads the access
// module's roles and bindings (see Alerts).
type Holders func(ctx context.Context, permission string) ([]int, error)

// Alerts configures the alert a new incident raises. The zero Alerts is off: nothing is queued or sent. Pass one to
// Install with Enabled set:
//
//	notices := notification.Install(app, users, incident.AlertCategory, notification.AppURL(url))
//	incident.Install(app, users, incident.Alerts{Enabled: true, Notices: notices})
//
// A new incident of an immediate severity is sent at once to the people who hold ManageIncidents, as a high priority
// notification of AlertCategory (the inbox, and e-mail unless a person turned it off). A second one of the same
// severity within Cooldown of the last that was sent waits for the digest instead, and so does an incident of any
// other severity: every DigestEvery the waiting ones are sent as one notification. The cooldown starts when a send
// succeeded, so an alert that could not be sent holds nothing back and is tried again.
type Alerts struct {
	// Enabled turns the alerts on.
	Enabled bool
	// Notices sends the alerts; required when Enabled. Its Install must have been given AlertCategory.
	Notices *notification.Notices
	// Immediate are the severities sent at once; the default is critical and high.
	Immediate []Severity
	// Cooldown is the time before a second immediate alert of one severity; the default is DefaultCooldown.
	Cooldown time.Duration
	// DigestEvery is how often the digest is sent; the default is DefaultDigestEvery.
	DigestEvery time.Duration
	// Holders names the people who hold the permission. The default finds them through the application's
	// authorizer, when it is the access module's engine, and the roles and bindings in the primary database.
	Holders Holders
}

func (a Alerts) apply(cfg *config) { cfg.alerts = a }

// ClientErrors sets the limit of the browser error report (POST /v1/incidents/client-errors): a person's reports are
// counted by the limiter under their id, and the ones over the limit are dropped with the same 204 as the others,
// since the browser has nothing to do about them. The default is DefaultClientErrorLimit per minute, in memory.
func ClientErrors(limiter ratelimit.Limiter) Option {
	return optionFunc(func(cfg *config) {
		switch {
		case limiter == nil:
			cfg.problem("incident.ClientErrors was given no limiter", "pass one, such as ratelimit.NewInMemoryLimiter(20, time.Minute)")
		case cfg.limiter != nil:
			cfg.problem("incident.ClientErrors was given twice", "keep one")
		default:
			cfg.limiter = limiter
		}
	})
}

// Retain sets how long the occurrences of an incident are kept, except the newest of each; the default is
// DefaultOccurrenceRetention.
func Retain(occurrences time.Duration) Option {
	return optionFunc(func(cfg *config) {
		if occurrences <= 0 {
			cfg.problem("incident.Retain was given "+occurrences.String(), "give a positive time, such as 90 * 24 * time.Hour")

			return
		}

		cfg.retain = occurrences
	})
}

// Link says where an incident opens in the application, as the path an alert links to: the default is "/incidents/<id>".
func Link(path func(incidentID int) string) Option {
	return optionFunc(func(cfg *config) {
		if path == nil {
			cfg.problem("incident.Link was given no function", "pass one that returns the in-app path of an incident")

			return
		}

		cfg.link = path
	})
}

type config struct {
	capture  Capture
	alerts   Alerts
	limiter  ratelimit.Limiter
	retain   time.Duration
	link     func(incidentID int) string
	problems []problem
}

type problem struct{ What, Fix string }

func (c *config) problem(what, fix string) { c.problems = append(c.problems, problem{what, fix}) }

// collect reads the options, fills the defaults and says what is wrong with them.
func collect(opts []Option) *config {
	cfg := &config{retain: DefaultOccurrenceRetention, link: defaultLink}

	for _, o := range opts {
		if o != nil {
			o.apply(cfg)
		}
	}

	if cfg.capture.TrailSize == 0 {
		cfg.capture.TrailSize = DefaultTrailSize
	}

	cfg.alerts = cfg.alerts.withDefaults()

	if cfg.alerts.Enabled && cfg.alerts.Notices == nil {
		cfg.problem("incident.Alerts is enabled but has no Notices",
			"pass the *notification.Notices that notification.Install returned, installed with incident.AlertCategory")
	}

	for _, s := range cfg.alerts.Immediate {
		if !s.Valid() {
			cfg.problem("incident.Alerts.Immediate has the severity "+strconv.Quote(string(s)),
				"use incident.SeverityLow, SeverityMedium, SeverityHigh or SeverityCritical")
		}
	}

	return cfg
}

func (a Alerts) withDefaults() Alerts {
	if a.Immediate == nil {
		a.Immediate = []Severity{SeverityCritical, SeverityHigh}
	}

	if a.Cooldown <= 0 {
		a.Cooldown = DefaultCooldown
	}

	if a.DigestEvery <= 0 {
		a.DigestEvery = DefaultDigestEvery
	}

	return a
}

func defaultLink(incidentID int) string { return fmt.Sprintf("/incidents/%d", incidentID) }

// immediate reports whether a severity is sent at once.
func (a Alerts) immediate(s Severity) bool {
	for _, have := range a.Immediate {
		if strings.EqualFold(string(have), string(s)) {
			return true
		}
	}

	return false
}
