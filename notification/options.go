package notification

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/wssto2/go-core/notification/push"
)

// Kind is a declared category: its code and what a person gets by default. Declare
// each category once, as a value, with Category, and register it with Install
// (NOTIF-CATEGORY-001). The in-app notification is always on; e-mail is off unless
// the category asks for it:
//
//	var TicketAssigned  = notification.Category("tickets.assigned").EmailByDefault() // e-mail on until the person turns it off
//	var TicketCommented = notification.Category("tickets.commented")                  // in-app only until the person turns e-mail on
//
// Push is off the same way, and on with PushByDefault. HighPriority marks a category a person should hear about at once:
// its pushes are urgent and live four hours instead of a day (NOTIF-DELIVERY-001).
//
// A Kind is a comparable value; Send takes one, and Install takes them in the same list as its options.
type Kind struct {
	code     string
	email    bool
	push     bool
	high     bool
	internal bool
}

// Category declares a category with its code: lower-case words joined by dots or
// dashes, at most CategoryMax characters, such as "tickets.assigned". A bad code is a
// start-up problem Install reports with the fix.
func Category(code string) Kind { return Kind{code: code} }

// EmailByDefault makes e-mail on for the category until a person turns it off (NOTIF-PREF-001, step 3).
// Without it e-mail is off until the person turns it on. The in-app notification is always made.
func (k Kind) EmailByDefault() Kind {
	k.email = true

	return k
}

// PushByDefault makes web push on for the category until a person turns it off (NOTIF-PREF-001, step 3). Without it push
// is off until the person turns it on. It matters only when Install was given WithPush.
func (k Kind) PushByDefault() Kind {
	k.push = true

	return k
}

// HighPriority marks the category as urgent: its pushes carry the Urgency "high" and are retried for four hours
// instead of twenty-four (NOTIF-DELIVERY-001, rule 2).
func (k Kind) HighPriority() Kind {
	k.high = true

	return k
}

// Code is the category's code, as stored with each notification and as it travels in the API.
func (k Kind) Code() string { return k.code }

// String is the category's code.
func (k Kind) String() string { return k.code }

// CategoryMax is the longest category code, the width of its column.
const CategoryMax = 64

// systemTest is the module's own category, for the test notification
// (POST /v1/notifications/test). It is reserved: an application cannot register it. It is
// internal: a person cannot configure it, it is never e-mailed and never held.
var systemTest = Kind{code: "system.test", internal: true}

var categoryPattern = regexp.MustCompile(`^[a-z][a-z0-9]*([.-][a-z0-9]+)*$`)

// Option is what Install takes after the people: a Kind, to register a category, or one of the
// options AppURL, TimeZone, Enforce and WithPush. They go in one list, in any order.
type Option interface{ apply(*config) }

type optionFunc func(*config)

func (f optionFunc) apply(c *config) { f(c) }

func (k Kind) apply(c *config) { c.kinds = append(c.kinds, k) }

// Channel is a way a notification reaches a person besides the inbox, which is always on.
type Channel string

// The channels a person can configure.
const (
	ChannelEmail Channel = emailChannel
	ChannelPush  Channel = pushChannel
)

// Enforcer is an outside authority over a person's setting of a channel, such as a dealer's policy: it says whether
// the channel of category k is on for the person and whether the person may not change it. When enforced is false the
// person's own choice, then the category's default, decides (NOTIF-PREF-001). An error stops the write that asked, and
// an event handler retries.
type Enforcer func(ctx context.Context, userID int, k Kind, ch Channel) (on, enforced bool, err error)

// AppURL is where the application is served, such as "https://tickets.example.com": the e-mail of a
// notification links to AppURL plus the message's in-app Link. It is required once e-mail is on (identity was
// given WithMail); Check reports its absence with the fix. A path is kept ("https://example.com/app"),
// a trailing slash is not.
func AppURL(base string) Option {
	return optionFunc(func(c *config) {
		if c.appURLSet {
			c.problem("notification.AppURL was given twice", "keep one")

			return
		}

		c.appURLSet = true

		u, err := url.Parse(strings.TrimSpace(base))

		switch {
		case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil:
			c.problem(fmt.Sprintf("notification.AppURL(%q) is not the address the application is served at", base),
				"give the origin, such as notification.AppURL(\"https://tickets.example.com\")")
		default:
			c.appURL = strings.TrimRight(u.String(), "/")
		}
	})
}

// TimeZone is the zone quiet hours are read in, on the wall clock, so a night with a daylight-saving
// change still ends at the hour the person chose (NOTIF-QUIET-001). The default is time.Local.
func TimeZone(loc *time.Location) Option {
	return optionFunc(func(c *config) {
		if loc == nil {
			c.problem("notification.TimeZone was given no location", "pass one, such as time.LoadLocation(\"Europe/Zagreb\")")

			return
		}

		if c.location != nil {
			c.problem("notification.TimeZone was given twice", "keep one")

			return
		}

		c.location = loc
	})
}

// Enforce sets the outside authority over the e-mail and push settings: see Enforcer. It is for an application whose
// administrators decide for their people; without it a person's own choice decides.
func Enforce(e Enforcer) Option {
	return optionFunc(func(c *config) {
		switch {
		case e == nil:
			c.problem("notification.Enforce was given no function", "pass a func(ctx, userID, kind, channel) (on, enforced bool, err error)")
		case c.enforce != nil:
			c.problem("notification.Enforce was given twice", "keep one")
		default:
			c.enforce = e
		}
	})
}

// WithPush turns on web push (NOTIF-DEVICE-001): people register their browsers as devices, and a notification is pushed
// to the devices of a person whose push setting for its category is on, held in their quiet hours, retried, and bundled
// in a burst. Without it the device routes answer that push is not configured and no push is made. The key pair is made
// once with push.GenerateKeys and kept stable: a new one invalidates every subscription.
func WithPush(c push.Config) Option {
	return optionFunc(func(cfg *config) {
		if cfg.push != nil {
			cfg.problem("notification.WithPush was given twice", "keep one")

			return
		}

		sender, err := push.NewSender(c)
		if err != nil {
			cfg.problem("notification.WithPush was given a configuration that cannot push: "+err.Error(),
				"give the key pair from push.GenerateKeys (public and private, base64url) and a subject, an https URL or an e-mail address")

			return
		}

		cfg.push = sender
	})
}

// PushBundleText writes the title and body of a bundled push (NOTIF-QUIET-001, rule 4): locale is the person's, count
// the notifications bundled and titles their titles, oldest first. The default says "N new notifications" in English,
// Croatian, Bosnian and Slovenian, and lists the newest three titles as the body.
func PushBundleText(f func(locale string, count int, titles []string) (title, body string)) Option {
	return optionFunc(func(cfg *config) {
		switch {
		case f == nil:
			cfg.problem("notification.PushBundleText was given no function", "pass a func(locale, count, titles) (title, body string)")
		case cfg.bundleText != nil:
			cfg.problem("notification.PushBundleText was given twice", "keep one")
		default:
			cfg.bundleText = f
		}
	})
}

// config is what Install collected from its options.
type config struct {
	kinds      []Kind
	appURL     string
	appURLSet  bool
	location   *time.Location
	enforce    Enforcer
	push       *push.Sender
	bundleText func(locale string, count int, titles []string) (title, body string)
	problems   []problem
}

func (c *config) problem(what, fix string) { c.problems = append(c.problems, problem{what, fix}) }

type problem struct{ What, Fix string }

// collect reads the options and says what is wrong with them.
func collect(opts []Option) *config {
	c := &config{}

	for _, o := range opts {
		if o != nil {
			o.apply(c)
		}
	}

	if c.location == nil {
		c.location = time.Local
	}

	c.problems = append(c.problems, kindProblems(c.kinds)...)

	return c
}

// kindProblems says what is wrong with the categories given to Install.
func kindProblems(kinds []Kind) []problem {
	var out []problem

	seen := map[string]bool{}

	for _, k := range kinds {
		switch {
		case !categoryPattern.MatchString(k.code) || len(k.code) > CategoryMax:
			out = append(out, problem{fmt.Sprintf("notification category %q is not lower-case words joined by dots or dashes, at most %d characters", k.code, CategoryMax),
				"declare it like notification.Category(\"tickets.assigned\")"})
		case k.code == systemTest.code:
			out = append(out, problem{fmt.Sprintf("notification category %q is the module's own", k.code), "choose another code for your category"})
		case seen[k.code]:
			out = append(out, problem{fmt.Sprintf("notification category %q is registered twice", k.code), "declare each category once and pass it to Install once"})
		}

		seen[k.code] = true
	}

	return out
}
