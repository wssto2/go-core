package notification

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/env"
	"github.com/wssto2/go-core/notification/push"
)

// Kind is a declared category: its code and what a person gets by default. Declare
// each category once, as a value, with Category, and register it with Install
// (NOTIF-CATEGORY-001). The in-app notification is always on; e-mail is off unless
// the category asks for it:
//
// var TicketAssigned = notification.Category("tickets.assigned").EmailByDefault() // e-mail on until the person turns
// it off var TicketCommented = notification.Category("tickets.commented") // in-app only until the person turns e-mail
// on
//
// it off var TicketCommented = notification.Category("tickets.commented") // in-app only until the person turns e-mail
// on
//
// Push is off the same way, and on with PushByDefault. HighPriority marks a category a person should hear about at
// once: its pushes are urgent and live four hours instead of a day (NOTIF-DELIVERY-001).
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

// PushByDefault makes web push on for the category until a person turns it off (NOTIF-PREF-001, step 3). Without it
// push is off until the person turns it on. It matters only when Install was given WithPush.
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

// WithCatalogue gives Install the application's permission catalogue: it defines the dead-letter permissions
// (ViewDeadLetters, RetryDeadLetters) there when the catalogue lacks them and hands the catalogue to app.Permissions,
// so nothing else is called for them. Access.Install defines the same two, so it may be installed before or after.
// Without it the application defines them itself, or the routes are a start-up problem.
func WithCatalogue(catalogue *authz.Catalogue) Option { //nolint:ireturn // an Option is the interface
	return optionFunc(func(c *config) { c.catalogue = catalogue })
}

// Channel is a way a notification reaches a person besides the inbox, which is always on.
type Channel string

// The channels a person can configure.
const (
	ChannelEmail Channel = emailChannel
	ChannelPush  Channel = pushChannel
)

// Enforcer is an outside authority over a person's setting of a channel, such as a dealer's policy: it says whether the
// channel of category k is on for the person and whether the person may not change it. When enforced is false the
// person's own choice, then the category's default, decides (NOTIF-PREF-001). An error stops the write that asked, and
// an event handler retries.
type Enforcer func(ctx context.Context, userID int, k Kind, ch Channel) (on, enforced bool, err error)

// AppURL is where the application is served, such as "https://tickets.example.com": the e-mail of a notification links
// to AppURL plus the message's in-app Link. It is required once e-mail is on (identity was given WithMail); Check
// reports its absence with the fix. A path is kept ("https://example.com/app"), a trailing slash is not.
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

// TestMessage words the test notification (POST /v1/notifications/test, and the test of one device) per recipient, so
// it is in the person's language (Recipient.Locale) and can link to the application's notification settings with
// Message.Link. Without it the message is English text and has no link: the module does not know where the
// application keeps its settings.
//
//nolint:ireturn // Option is the type of every option of Install
func TestMessage(text func(Recipient) Message) Option {
	return optionFunc(func(cfg *config) {
		switch {
		case text == nil:
			cfg.problem("notification.TestMessage was given no function", "pass one that returns the Message for a Recipient")
		case cfg.testText != nil:
			cfg.problem("notification.TestMessage was given twice", "keep one")
		default:
			cfg.testText = text
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
			c.problem(
				"notification.Enforce was given no function",
				"pass a func(ctx, userID, kind, channel) (on, enforced bool, err error)",
			)
		case c.enforce != nil:
			c.problem("notification.Enforce was given twice", "keep one")
		default:
			c.enforce = e
		}
	})
}

// WithOff silences categories for everyone (NOTIF-OFF-001): e-mail and push are "off, enforced" for them, ahead of
// Enforce, and the inbox entry is still written, because it is not a channel Enforce governs. It is the operator's
// kill switch for a noisy category. Install refuses a code that is not a registered category, naming it, so a typo
// cannot leave a category on. It may be given more than once.
func WithOff(codes ...string) Option { //nolint:ireturn // Option is the type of every option of Install
	return optionFunc(func(c *config) { c.off = append(c.off, codes...) })
}

// OffEnv is the setting OffFromEnv reads.
const OffEnv = "NOTIFICATIONS_OFF"

// OffFromEnv is WithOff over the comma-separated list in NOTIFICATIONS_OFF, read through the application's reader
// (app.Env()). Unset silences nothing.
func OffFromEnv(vars *env.Reader) Option { //nolint:ireturn // Option is the type of every option of Install
	return WithOff(vars.List(OffEnv)...)
}

// WithPush turns on web push (NOTIF-DEVICE-001): people register their browsers as devices, and a notification is
// pushed to the devices of a person whose push setting for its category is on, held in their quiet hours, retried, and
// bundled in a burst. Without it the device routes answer that push is not configured and no push is made. The key pair
// is made once with push.GenerateKeys and kept stable: a new one invalidates every subscription.
func WithPush(pushConf push.Config) Option { //nolint:ireturn // Option is the type of every option of Install
	return optionFunc(func(cfg *config) {
		if cfg.push != nil {
			cfg.problem("notification.WithPush was given twice", "keep one")

			return
		}

		sender, err := push.NewSender(pushConf)
		if err != nil {
			cfg.problem(
				"notification.WithPush was given a configuration that cannot push: "+err.Error(),
				"give the key pair from push.GenerateKeys (public and private, base64url) and a subject, "+
					"an https URL or an e-mail address",
			)

			return
		}

		cfg.push = sender
	})
}

// PushBundleText writes the title and body of a bundled push (NOTIF-QUIET-001, rule 4): locale is the person's, count
// the notifications bundled and titles their titles, oldest first. The default says "N new notifications" in English,
// Croatian, Bosnian and Slovenian, and lists the newest three titles as the body.
//
//nolint:ireturn // Option is the type of every option of Install
func PushBundleText(text func(locale string, count int, titles []string) (title, body string)) Option {
	return optionFunc(func(cfg *config) {
		switch {
		case text == nil:
			cfg.problem(
				"notification.PushBundleText was given no function",
				"pass a func(locale, count, titles) (title, body string)",
			)
		case cfg.bundleText != nil:
			cfg.problem("notification.PushBundleText was given twice", "keep one")
		default:
			cfg.bundleText = text
		}
	})
}

// config is what Install collected from its options.
type config struct {
	catalogue  *authz.Catalogue
	kinds      []Kind
	appURL     string
	appURLSet  bool
	location   *time.Location
	enforce    Enforcer
	off        []string
	push       *push.Sender
	bundleText func(locale string, count int, titles []string) (title, body string)
	testText   func(Recipient) Message
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
	c.problems = append(c.problems, offProblems(c.off, c.kinds)...)
	c.enforce = withOff(c.off, c.enforce)

	return c
}

// offProblems names each silenced code that is not a registered category.
func offProblems(off []string, kinds []Kind) []problem {
	var out []problem

	known := make([]string, 0, len(kinds))
	for _, k := range kinds {
		known = append(known, k.code)
	}

	for _, code := range off {
		if !slices.Contains(known, code) {
			what := fmt.Sprintf("%s %q is not a notification category (known: %s)", OffEnv, code, strings.Join(known, ", "))
			out = append(out, problem{what, "name a category given to notification.Install, or remove it from the list"})
		}
	}

	return out
}

// withOff answers "off, enforced" on e-mail and push for a silenced category and leaves every other question to next,
// which may be nil.
func withOff(off []string, next Enforcer) Enforcer {
	if len(off) == 0 {
		return next
	}

	return func(ctx context.Context, userID int, kind Kind, channel Channel) (bool, bool, error) {
		if slices.Contains(off, kind.code) {
			return false, true, nil
		}

		if next == nil {
			return false, false, nil
		}

		return next(ctx, userID, kind, channel)
	}
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
