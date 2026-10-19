//nolint:err113,exhaustruct,exhaustruct_v5,goconst,lll,paralleltest,varnamelen // tests in the style of the package's others
package incident_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/authz"
	authzstore "github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/incident"
	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/incident/gormstore"
	"github.com/wssto2/go-core/notification"
)

// The managers of the alert tests: 7 reads Croatian, 8 English; 9 does not manage.
const (
	managerHr = 7
	managerEn = 8
	nobodyMgr = 9
)

// notified are the people notification can tell: the accounts of the alert tests.
type notified struct{}

func (notified) Find(_ context.Context, ids []int) ([]account.Account, error) {
	all := map[int]account.Account{
		managerHr: {ID: managerHr, Login: "marko", Name: "Marko", Locale: "hr", Active: true},
		managerEn: {ID: managerEn, Login: "mary", Name: "Mary", Locale: "en", Active: true},
		nobodyMgr: {ID: nobodyMgr, Login: "nina", Name: "Nina", Locale: "en", Active: true},
	}

	var out []account.Account

	for _, id := range ids {
		if a, ok := all[id]; ok {
			out = append(out, a)
		}
	}

	return out, nil
}

func (notified) Sessions(context.Context, int) ([]account.Session, error) { return nil, nil }

// alertWorld is an application with notifications and the module, on a clock the test moves.
type alertWorld struct {
	t       *testing.T
	app     *gocore.App
	notices *notification.Notices
	inc     *incident.Incidents
	store   *gormstore.Store
	now     time.Time
	holders []int
	failing bool
}

func newAlertWorld(t *testing.T, alerts incident.Alerts) *alertWorld {
	t.Helper()

	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	w := &alertWorld{t: t, now: time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC), holders: []int{managerHr, managerEn}}

	app := gocore.New(
		gocore.WithRegistry(reg), gocore.WithAutoMigrate(t.Context()), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithAuthentication(signIn), gocore.WithAuthorizer(permissions), gocore.WithClock(fixedClock{at: &w.now}))

	catalogue := authz.NewCatalogue()
	require.NoError(t, incident.DefinePermissions(catalogue))
	require.NoError(t, notification.DefinePermissions(catalogue))
	app.Permissions(catalogue)

	w.notices = notification.Install(app, notified{})

	if alerts.Holders == nil {
		alerts.Holders = func(context.Context, string) ([]int, error) {
			if w.failing {
				return nil, errors.New("the roles cannot be read")
			}

			return w.holders, nil
		}
	}

	w.inc = incident.Install(app, people(), w.notices, alerts)
	w.app, w.store = app, gormstore.New(app.Database())

	_, err := app.Handler()
	require.NoError(t, err)

	return w
}

// failure captures a server error that is a new incident when its message is new.
func (w *alertWorld) failure(message string, severity core.Severity) {
	w.t.Helper()

	require.NoError(w.t, w.inc.Service.Capture(w.t.Context(), core.Failure{
		Message: message, Severity: severity, AffectedModule: "alerts", AffectedRoute: "/x",
	}))
}

func (w *alertWorld) drain() { require.NoError(w.t, w.app.DrainEvents(w.t.Context())) }

func (w *alertWorld) inbox(person int) []notification.Item {
	w.t.Helper()

	page, err := w.notices.Inbox.List(w.t.Context(), person, notification.ListQuery{})
	require.NoError(w.t, err)

	return page.Items
}

func (w *alertWorld) waiting() int {
	var n int64

	require.NoError(w.t, w.app.Database().Table("incident_digest_queue").Where("digested_at IS NULL").Count(&n).Error)

	return int(n)
}

func TestANewIncidentIsSentAtOnceToTheHoldersOfManage(t *testing.T) {
	w := newAlertWorld(t, incident.Alerts{Enabled: true})
	w.failure("first failure", core.SeverityHigh)
	w.drain()

	for _, person := range []int{managerHr, managerEn} {
		items := w.inbox(person)
		require.Len(t, items, 1, "person %d", person)
		assert.Equal(t, "incident.alert", items[0].Category)
		assert.Equal(t, "/incidents/1", items[0].Link)
		assert.Equal(t, "INC-", items[0].Data["public_code"][:4])
		assert.Equal(t, "high", items[0].Data["severity"])
	}

	assert.Contains(t, w.inbox(managerEn)[0].Title, "New high incident: Unhandled error in alerts")
	assert.Contains(t, w.inbox(managerHr)[0].Title, "Novi visok incident: ", "in the person's language")
	assert.Contains(t, w.inbox(managerHr)[0].Body, "Šifra: INC-")
	assert.Empty(t, w.inbox(nobodyMgr))
	assert.Zero(t, w.waiting())

	at, found, err := w.store.LastAlert(t.Context(), "high")
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, at.Equal(w.now))
}

func TestTheCooldownHoldsBackTheNextAlertOfASeverityAndSendsItInTheDigest(t *testing.T) {
	w := newAlertWorld(t, incident.Alerts{Enabled: true})
	w.failure("first failure", core.SeverityHigh)
	w.drain()

	w.now = w.now.Add(time.Minute)
	w.failure("second failure", core.SeverityHigh)
	w.drain()

	assert.Len(t, w.inbox(managerEn), 1, "the second was held back")
	assert.Equal(t, 1, w.waiting(), "and is waiting for the digest")

	w.failure("a critical one", core.SeverityCritical)
	w.drain()
	assert.Len(t, w.inbox(managerEn), 2, "another severity has a cooldown of its own")

	w.now = w.now.Add(10 * time.Minute)
	w.failure("third failure", core.SeverityHigh)
	w.drain()
	assert.Len(t, w.inbox(managerEn), 3, "once the cooldown is over the next one is sent")
}

func TestTheOtherSeveritiesWaitForTheDigestWhichIsOneAlert(t *testing.T) {
	w := newAlertWorld(t, incident.Alerts{Enabled: true})
	w.failure("a slow one", core.SeverityMedium)
	w.failure("a small one", core.SeverityLow)
	w.drain()

	assert.Empty(t, w.inbox(managerEn))
	assert.Equal(t, 2, w.waiting())

	require.NoError(t, w.inc.FlushDigest(t.Context()))
	assert.Zero(t, w.waiting(), "the incidents are taken out of the queue in the transaction that queues the digest")
	w.drain()

	items := w.inbox(managerEn)
	require.Len(t, items, 1)
	assert.Equal(t, "New incidents: 2", items[0].Title)
	assert.Contains(t, items[0].Body, "[MEDIUM] Unhandled error in alerts")
	assert.Contains(t, items[0].Body, "[LOW]")
	assert.Equal(t, "Novi incidenti: 2", w.inbox(managerHr)[0].Title)

	require.NoError(t, w.inc.FlushDigest(t.Context()))
	w.drain()
	assert.Len(t, w.inbox(managerEn), 1, "an empty queue sends nothing")

	// The sent items are deleted a day later.
	swept, err := w.inc.Sweep(t.Context())
	require.NoError(t, err)
	assert.Zero(t, swept.Digest)

	w.now = w.now.Add(25 * time.Hour)
	swept, err = w.inc.Sweep(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, swept.Digest)
}

func TestTheImmediateSeveritiesAreOptions(t *testing.T) {
	w := newAlertWorld(t, incident.Alerts{Enabled: true, Immediate: []incident.Severity{incident.SeverityLow}, Cooldown: time.Minute})
	w.failure("a high one", core.SeverityHigh)
	w.failure("a low one", core.SeverityLow)
	w.drain()

	require.Len(t, w.inbox(managerEn), 1)
	assert.Contains(t, w.inbox(managerEn)[0].Title, "New low incident")
	assert.Equal(t, 1, w.waiting())
}

func TestACooldownIsStartedOnlyByASendThatSucceeded(t *testing.T) {
	w := newAlertWorld(t, incident.Alerts{Enabled: true})
	w.failing = true
	w.failure("first failure", core.SeverityHigh)
	require.Error(t, w.app.DrainEvents(t.Context()), "the consumer failed: the event is kept for a retry")

	_, found, err := w.store.LastAlert(t.Context(), "high")
	require.NoError(t, err)
	assert.False(t, found, "no cooldown was started by an alert that was not sent")
	assert.Empty(t, w.inbox(managerEn))
	assert.Zero(t, w.waiting(), "and it did not go to the digest either: it is tried again")

	// The failure is gone; the event is delivered again after its backoff and the alert goes out.
	w.failing = false
	w.now = w.now.Add(time.Minute)
	w.drain()

	assert.Len(t, w.inbox(managerEn), 1)

	_, found, err = w.store.LastAlert(t.Context(), "high")
	require.NoError(t, err)
	assert.True(t, found)
}

func TestNobodyToTellIsNotACooldown(t *testing.T) {
	w := newAlertWorld(t, incident.Alerts{Enabled: true})
	w.holders = nil
	w.failure("first failure", core.SeverityHigh)
	w.drain()

	_, found, err := w.store.LastAlert(t.Context(), "high")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestTheAlertsAreOffByDefaultButTheEventIsPublished(t *testing.T) {
	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	now := time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)
	app := gocore.New(
		gocore.WithRegistry(reg), gocore.WithAutoMigrate(t.Context()), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithAuthentication(signIn), gocore.WithAuthorizer(permissions), gocore.WithClock(fixedClock{at: &now}))

	var seen []incident.NewIncident

	app.Events(incident.Opened.To("test.collect", func(_ context.Context, e incident.NewIncident) error {
		seen = append(seen, e)

		return nil
	}))

	inc := incident.Install(app, people(), nil)

	require.NoError(t, inc.Service.Capture(t.Context(), core.Failure{Message: "boom", AffectedModule: "x"}))
	require.NoError(t, inc.Service.Capture(t.Context(), core.Failure{Message: "boom", AffectedModule: "x"}))
	require.NoError(t, app.DrainEvents(t.Context()))

	require.Len(t, seen, 1, "only a new incident is announced, once")
	assert.Equal(t, "high", seen[0].Severity)
	assert.Equal(t, "runtime_error", seen[0].Source)
	assert.Equal(t, 1, seen[0].IncidentID)

	var queued int64
	require.NoError(t, app.Database().Table("incident_digest_queue").Count(&queued).Error)
	assert.Zero(t, queued)
}

func TestAnEnabledAlertNeedsNotices(t *testing.T) {
	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	app := gocore.New(gocore.WithRegistry(reg), gocore.WithAutoMigrate(t.Context()),
		gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithAuthentication(signIn), gocore.WithAuthorizer(permissions))
	catalogue := authz.NewCatalogue()
	require.NoError(t, incident.DefinePermissions(catalogue))
	app.Permissions(catalogue)

	incident.Install(app, people(), nil, incident.Alerts{Enabled: true, Immediate: []incident.Severity{"bogus"}})

	err := app.Check()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "incident.Alerts is enabled but incident.Install has no Notices")
	assert.Contains(t, err.Error(), `"bogus"`)
}

// team is the SubjectDirectory of the access test: people 1 to 20.
type team struct{}

func (team) SubjectNames(_ context.Context, subjects []authz.Subject) (map[authz.Subject]string, error) {
	out := map[authz.Subject]string{}

	for _, s := range subjects {
		out[s] = "Subject " + s.String()
	}

	return out, nil
}

// By default the alert goes to the people the access module's roles give incident:manage, wherever it is given:
// a predefined role or a custom one, never a service account and never a role without it.
func TestTheDefaultRecipientsAreTheHoldersOfManageInTheAccessRoles(t *testing.T) {
	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	now := time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)
	app := gocore.New(
		gocore.WithRegistry(reg), gocore.WithAutoMigrate(t.Context()), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithAuthentication(signIn), gocore.WithClock(fixedClock{at: &now}))

	catalogue := authz.NewCatalogue()
	require.NoError(t, incident.DefinePermissions(catalogue))
	require.NoError(t, notification.DefinePermissions(catalogue))

	reading := authz.Grants(authz.QualifierAll, incident.ViewIncidents)
	managing := authz.Grants(authz.QualifierAll, incident.ViewIncidents, incident.ManageIncidents)

	acc := access.Install(app, catalogue, team{}, access.WithRoles(
		authz.Role{Key: "support", Name: "Support", Grants: managing},
		authz.Role{Key: "reader", Name: "Reader", Grants: reading},
	))

	notices := notification.Install(app, notified{})
	inc := incident.Install(app, people(), notices, incident.Alerts{Enabled: true})

	_, err := app.Handler()
	require.NoError(t, err)

	ctx := t.Context()
	user := func(id int) authz.Subject { return authz.Subject{Kind: authz.KindUser, ID: id} }

	require.NoError(t, acc.Seed(ctx, user(managerHr), "support"))
	require.NoError(t, acc.Seed(ctx, user(nobodyMgr), "reader"))
	require.NoError(t, acc.Seed(ctx, authz.Subject{Kind: authz.KindServiceAccount, ID: 5}, "support"))

	store := authzstore.New(app.Database())
	custom, err := store.SaveRole(ctx, authz.Subject{}, authz.Role{Name: "Lead", Grants: managing})
	require.NoError(t, err)
	_, err = store.Bind(ctx, authz.Subject{}, authz.Binding{Subject: user(managerEn), Role: custom.Ref(), Scope: acc.Engine.Hierarchy().Root()})
	require.NoError(t, err)

	require.NoError(t, inc.Service.Capture(ctx, core.Failure{Message: "boom", AffectedModule: "alerts"}))
	require.NoError(t, app.DrainEvents(ctx))

	page := func(person int) int {
		items, err := notices.Inbox.List(ctx, person, notification.ListQuery{})
		require.NoError(t, err)

		return len(items.Items)
	}

	assert.Equal(t, 1, page(managerHr), "a predefined role")
	assert.Equal(t, 1, page(managerEn), "a custom role")
	assert.Zero(t, page(nobodyMgr), "a role without incident:manage")
}

// An application whose authorizer is not the access module's engine says who holds the permission itself.
func TestWithoutTheAccessEngineHoldersAreRequired(t *testing.T) {
	w := newAlertWorld(t, incident.Alerts{Enabled: true, Holders: func(context.Context, string) ([]int, error) { return []int{managerEn}, nil }})
	w.failure("boom", core.SeverityHigh)
	w.drain()
	assert.Len(t, w.inbox(managerEn), 1)
	assert.Empty(t, w.inbox(managerHr))
}

// The module registers its own category: an application installs notification without AlertCategory and the alert
// still goes out, and the category is among the person's settings.
func TestInstallRegistersTheAlertCategory(t *testing.T) {
	w := newAlertWorld(t, incident.Alerts{Enabled: true})

	w.failure("boom", core.SeverityHigh)
	w.drain()

	assert.Len(t, w.inbox(managerHr), 1)

	prefs, err := w.notices.Settings.Get(t.Context(), managerHr)
	require.NoError(t, err)

	codes := make([]string, 0, len(prefs.Categories))
	for _, c := range prefs.Categories {
		codes = append(codes, c.Category)
	}

	assert.Contains(t, codes, incident.AlertCategory.Code())
}

// An application that still passes AlertCategory to notification.Install keeps working.
func TestAnApplicationThatRegisteredTheCategoryItselfStillWorks(t *testing.T) {
	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	app := gocore.New(bootstrap.DefaultConfig(), gocore.WithRegistry(reg), gocore.WithAutoMigrate(t.Context()),
		gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithAuthentication(signIn), gocore.WithAuthorizer(permissions))
	catalogue := authz.NewCatalogue()
	require.NoError(t, incident.DefinePermissions(catalogue))
	require.NoError(t, notification.DefinePermissions(catalogue))
	app.Permissions(catalogue)

	notices := notification.Install(app, notified{}, incident.AlertCategory)
	incident.Install(app, people(), notices, incident.Alerts{Enabled: true})

	require.NoError(t, app.Check())
}
