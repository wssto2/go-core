// Package incident is the incident log of an application: a server error becomes an incident, repeats of the same
// error are counted on it, a person can report a bug, the browser can report an error, and the people who run the
// application see the list, work on the incidents and are told of the new ones.
//
//	incident.DefinePermissions(catalogue)
//	notices := notification.Install(app, users, notification.AppURL(url))
//	incidents := incident.Install(app, users, notices, incident.Alerts{Enabled: true})
//
// What Install puts in the application:
//
//   - the routes under /v1/incidents (Routes): the list, one incident, its comments, a bug report, a comment, a change,
//     and the browser error report. Reading is incident:view, filing and commenting incident:create, changing
//     incident:update. A holder of incident:manage is the staff and sees every incident; anybody else sees the shared
//     ones they reported, wrote a report on or watch.
//   - the capture, engine middleware that runs before every route: a response of 500 or more from the errors the
//     request collected, and a panic (recorded and raised again, so the answer does not change), each recorded in a
//     transaction of its own with the request id, the route, the caller (a person or a service account), the tenant,
//     the headers with their credentials redacted and the caller's last calls (INC-TRAIL-001). Repeats of one error
//     are grouped by a fingerprint of the module, route, error and where it happened (INC-FINGERPRINT-001) under one
//     open incident, which counts them.
//   - the alerts, when Alerts is enabled: a new incident of an immediate severity is sent to the people who hold
//     incident:manage, the rest wait for a digest (see Alerts).
//   - a housekeeper that deletes occurrences older than 90 days, except the newest of each incident.
//
// The browser reports its errors to POST /v1/incidents/client-errors. They become low severity incidents of their own
// source, grouped by message and top stack frame.
package incident

import (
	"context"
	"time"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/incident/gormstore"
	"github.com/wssto2/go-core/incident/migrations"
	"github.com/wssto2/go-core/notification"
	"github.com/wssto2/go-core/ratelimit"
	"gorm.io/gorm"
)

// People is what the module asks of the application's users: the accounts of many ids, to name the reporter, the
// assignee and the authors. *identity.Users is one.
type People interface {
	// Find returns the accounts that exist among ids, active or not, in one query.
	Find(ctx context.Context, ids []int) ([]account.Account, error)
}

// Incidents is what Install built. After Install reported a problem to the application (Run lists it) the value is
// empty.
type Incidents struct {
	// Service lists, shows, files, comments, changes and captures incidents. Its Capture records a failure an
	// application found by itself (a failed job), the same way the middleware does a failed request.
	Service *core.Service

	housekeeper housekeeper
	digest      digestWorker
}

// Install puts the incident log into app: the tables (their migrations, which the application runs with its migrate
// command), the capture before every route, the routes, the housekeeper and, with Alerts, the alerts. people names the
// people on an incident.
//
//	incidents := incident.Install(app, users, nil)
//	incidents := incident.Install(app, users, notices,
//		incident.Capture{TrailSize: 50}, incident.Alerts{Enabled: true})
//
// notices is the notification module the alerts go through; Install registers AlertCategory with it, so the
// application does not. It may be nil when the alerts are off.
//
// A bad option is a start-up problem Run lists with the fix.
func Install(app *gocore.App, people People, notices *notification.Notices, opts ...Option) *Incidents {
	defer app.Shipping()()

	if people == nil {
		app.Fail("incident needs the people to name", "pass the users as the second argument: incident.Install(app, users, notices)")

		return new(Incidents)
	}

	cfg := collect(opts, notices)
	if len(cfg.problems) > 0 {
		for _, p := range cfg.problems {
			app.Fail(p.What, p.Fix)
		}

		return new(Incidents)
	}

	if notices != nil {
		notices.Register(AlertCategory)
	}

	conn := app.Database()
	store := gormstore.New(conn)

	app.Schema(gocore.Schema{Files: migrations.Files, Models: gormstore.Migrate})

	service := core.New(core.Config{
		Store: store, Transactor: database.NewTransactor(conn), People: directory{people}, Clock: app.Clock(),
		Opened: publishOpened, Environment: cfg.capture.Environment,
	})

	app.Use(newCapturer(app, service, cfg.capture).handle)

	limiter := cfg.limiter
	if limiter == nil {
		limiter = ratelimit.NewInMemoryLimiter(DefaultClientErrorLimit, time.Minute)
	}

	app.Routes(Declare().To(&Handlers{
		service: service, authorizer: app.Authorizer, limiter: limiter, log: app.Logger(),
	})...)

	house := housekeeper{store: store, retain: cfg.retain, clock: app.Clock(), log: app.Logger()}
	app.Background(house)

	consumers, digest := installAlerts(app, cfg, notices, conn, store)
	app.Events(consumers...)

	return &Incidents{Service: service, housekeeper: house, digest: digest}
}

// installAlerts starts the digest worker and returns the consumers of the alerts, none when they are off.
func installAlerts(app *gocore.App, cfg *config, notices *notification.Notices, conn *gorm.DB, store core.Store) ([]event.Consumer, digestWorker) {
	if !cfg.alerts.Enabled {
		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		return nil, digestWorker{}
	}

	holders := cfg.alerts.Holders
	if holders == nil {
		holders = engineHolders(app)
	}

	alerts := &alerter{
		cfg: cfg.alerts, notices: notices, store: store, clock: app.Clock(), log: app.Logger(), holders: holders, link: cfg.link,
	}

	digest := digestWorker{
		every: cfg.alerts.DigestEvery, store: store, tx: database.NewTransactor(conn), clock: app.Clock(),
		log: app.Logger(),
	}

	app.Background(digest)

	return alerts.consumers(), digest
}

// Sweep deletes the old rows now: occurrences older than the retention except the newest of each incident, and the
// digest items that were sent. The housekeeper Install starts does it every hour; a command can call this to prune at
// once.
func (i *Incidents) Sweep(ctx context.Context) (Swept, error) { return i.housekeeper.sweep(ctx) }

// FlushDigest sends the digest now: the incidents that wait are taken out of the queue and queued as one alert. The
// worker Install starts does it every Alerts.DigestEvery; a command can call this to send at once. Without Alerts it
// does nothing.
func (i *Incidents) FlushDigest(ctx context.Context) error {
	if i.digest.store == nil {
		return nil
	}

	return i.digest.flush(ctx)
}

// directory adapts the application's people to the service's.
type directory struct{ people People }

// Find implements core.People.
func (d directory) Find(ctx context.Context, ids []int) ([]core.Person, error) {
	accounts, err := d.people.Find(ctx, ids)
	if err != nil {
		return nil, err //nolint:wrapcheck // the service names the lookup
	}

	out := make([]core.Person, 0, len(accounts))

	for _, person := range accounts {
		name := person.Name
		if name == "" {
			name = person.Login
		}

		out = append(out, core.Person{ID: person.ID, Name: name, Email: person.Email})
	}

	return out, nil
}
