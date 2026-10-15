package incident

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/incident/mailtext"
	"github.com/wssto2/go-core/notification"
)

// AlertCategory is the notification category of the alerts. Register it with notification.Install, beside the
// application's own categories: the alert is made for the people who hold ManageIncidents, in their inbox and, unless
// they turned it off, by e-mail, and it is high priority so that it is not held for quiet hours.
//
//nolint:gochecknoglobals // a category is declared once, as a value
var AlertCategory = notification.Category("incident.alert").EmailByDefault().HighPriority()

// Opened is published when a new incident is created, in the transaction that creates it, whether or not the alerts
// are on: an application may consume it (to open a ticket, to post to a channel). Repeats of an incident, and a closed
// one that is seen again, are not new incidents.
//
//nolint:gochecknoglobals // an event is declared once, as a value
var Opened = event.Define[NewIncident]("incident.opened")

// NewIncident is the payload of Opened.
//
//nolint:tagliatelle // snake_case wire
type NewIncident struct {
	IncidentID int    `json:"incident_id"`
	PublicCode string `json:"public_code"`
	Title      string `json:"title"`
	Severity   string `json:"severity"`
	Source     string `json:"source"`
}

// digest is published by the digest worker, with the incidents it took out of the waiting queue.
//
//nolint:gochecknoglobals // an event is declared once, as a value
var digested = event.Define[digest]("incident.digest")

type digest struct {
	Items []NewIncident `json:"items"`
}

// digestBatch is the most incidents one digest alert lists.
const digestBatch = 100

// opened is the hook of the service: it queues the Opened event in the transaction that created the incident.
func publishOpened(ctx context.Context, opened core.Opened) error {
	err := Opened.Publish(ctx, NewIncident{
		IncidentID: opened.IncidentID, PublicCode: opened.PublicCode, Title: opened.Title,
		Severity: string(opened.Severity), Source: string(opened.Source),
	})
	if err != nil {
		return fmt.Errorf("incident: queue the alert: %w", err)
	}

	return nil
}

// alerter sends the alerts: the consumer of Opened and of the digest.
type alerter struct {
	cfg     Alerts
	store   core.Store
	clock   gocore.Clock
	log     *slog.Logger
	holders Holders
	link    func(incidentID int) string
}

// consumers are the event consumers of the alerts.
func (a *alerter) consumers() []event.Consumer {
	return []event.Consumer{
		Opened.To("incident.alert", a.onOpened),
		digested.To("incident.digest", a.onDigest),
	}
}

// onOpened sends the alert of a new incident now if its severity is an immediate one and the cooldown of the severity
// is over, else queues it for the digest. The cooldown starts only when the send succeeded: a send that failed holds
// nothing back, and the event is delivered again.
func (a *alerter) onOpened(ctx context.Context, incident NewIncident) error {
	severity := core.Severity(strings.ToLower(incident.Severity))

	if a.cfg.immediate(severity) {
		sent, err := a.sendNow(ctx, severity, incident)
		if err != nil || sent {
			return err
		}
	}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	err := a.store.QueueDigest(ctx, core.DigestItem{
		IncidentID: incident.IncidentID, PublicCode: incident.PublicCode, Title: incident.Title, Severity: severity,
		CreatedAt: a.clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("incident: queue for the digest: %w", err)
	}

	return nil
}

// sendNow sends the alert of an incident of an immediate severity unless the severity is cooling. It says whether the
// alert was dealt with: sent now, or dropped because nobody is to be told (and no cooldown starts).
func (a *alerter) sendNow(ctx context.Context, severity core.Severity, incident NewIncident) (bool, error) {
	cool, err := a.cooling(ctx, string(severity))
	if err != nil || cool {
		return false, err
	}

	sent, err := a.send(ctx, func(locale string) (string, string) {
		return mailtext.Opened(locale, alertOf(incident))
	}, incident)
	if err != nil {
		return false, err
	}

	if sent {
		err = a.stamp(ctx, string(severity))
	}

	return true, err
}

// cooling reports whether an immediate alert of the severity was sent within the cooldown.
func (a *alerter) cooling(ctx context.Context, tier string) (bool, error) {
	last, found, err := a.store.LastAlert(ctx, tier)
	if err != nil {
		return false, fmt.Errorf("incident: read the cooldown: %w", err)
	}

	return found && a.clock.Now().Before(last.Add(a.cfg.Cooldown)), nil
}

func (a *alerter) stamp(ctx context.Context, tier string) error {
	err := a.store.StampAlert(ctx, tier, a.clock.Now())
	if err != nil {
		return fmt.Errorf("incident: start the cooldown: %w", err)
	}

	return nil
}

// onDigest sends the alert of the incidents that waited.
func (a *alerter) onDigest(ctx context.Context, batch digest) error {
	alerts := make([]mailtext.Alert, len(batch.Items))
	for i, item := range batch.Items {
		alerts[i] = alertOf(item)
	}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	linked := NewIncident{}
	if len(batch.Items) == 1 {
		linked = batch.Items[0]
	}

	_, err := a.send(ctx, func(locale string) (string, string) { return mailtext.Digest(locale, alerts) }, linked)

	return err
}

func alertOf(incident NewIncident) mailtext.Alert {
	return mailtext.Alert{
		PublicCode: incident.PublicCode, Title: incident.Title, Severity: incident.Severity, Source: incident.Source,
	}
}

// send makes the alert for the people who hold ManageIncidents. It reports false when there is nobody to tell. The
// notification links to the incident when the alert is about one.
func (a *alerter) send(
	ctx context.Context, words func(locale string) (string, string), about NewIncident,
) (bool, error) {
	people, err := a.holders(ctx, ManageIncidents)
	if err != nil {
		return false, fmt.Errorf("incident: find the people who hold %s: %w", ManageIncidents, err)
	}

	if len(people) == 0 {
		a.log.WarnContext(ctx, "incident: nobody holds the permission, so the alert is dropped",
			"permission", ManageIncidents)

		return false, nil
	}

	link := ""
	data := map[string]string{}

	if about.IncidentID > 0 {
		link = a.link(about.IncidentID)
		data = map[string]string{
			"incident_id": strconv.Itoa(about.IncidentID), "public_code": about.PublicCode, "severity": about.Severity,
		}
	}

	render := func(who notification.Recipient) notification.Message {
		title, body := words(who.Locale)

		return notification.Message{Title: title, Body: body, Link: link, Data: data}
	}

	err = a.cfg.Notices.Send(ctx, AlertCategory, notification.To(people...), render)
	if err != nil {
		return false, fmt.Errorf("incident: send the alert: %w", err)
	}

	return true, nil
}

// digestWorker takes the waiting incidents out of the queue every DigestEvery and publishes them as one digest event,
// in the transaction that marks them taken, so an incident is in one digest only and a digest that could not be sent
// is retried with the event.
type digestWorker struct {
	every time.Duration
	store core.Store
	tx    database.Transactor
	clock gocore.Clock
	log   *slog.Logger
}

// Name identifies the worker in logs and metrics.
func (digestWorker) Name() string { return "incident.digest" }

// Run flushes the digest until ctx is cancelled.
func (w digestWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			err := w.flush(ctx)
			if err != nil && ctx.Err() == nil {
				w.log.ErrorContext(ctx, "incident: digest failed", "error", err)
			}
		}
	}
}

// flush publishes the waiting incidents, a batch at a time.
func (w digestWorker) flush(ctx context.Context) error {
	for {
		var taken int

		err := w.tx.WithinTransaction(ctx, func(ctx context.Context) error {
			items, err := w.store.ClaimDigest(ctx, w.clock.Now(), digestBatch)
			if err != nil {
				return fmt.Errorf("claim the digest: %w", err)
			}

			taken = len(items)
			if taken == 0 {
				return nil
			}

			batch := digest{Items: make([]NewIncident, 0, taken)}
			for _, item := range items {
				//nolint:exhaustruct,exhaustruct_v5 // zero by design
				batch.Items = append(batch.Items, NewIncident{
					IncidentID: item.IncidentID, PublicCode: item.PublicCode, Title: item.Title, Severity: string(item.Severity),
				})
			}

			return digested.Publish(ctx, batch)
		})
		if err != nil {
			return fmt.Errorf("incident: digest: %w", err)
		}

		if taken < digestBatch {
			return nil
		}
	}
}

var errNoEngine = errors.New("incident: the application's authorizer cannot say who holds a permission " +
	"(it is not the access module's engine): give incident.Alerts a Holders function")

// holderLister is what the access module's authz.Engine answers: who holds a permission.
type holderLister interface {
	HoldersOf(ctx context.Context, permission string) ([]authz.Subject, error)
}

// engineHolders finds the people who hold a permission through the application's authorizer, when it is the access
// module's engine (authz.Engine.HoldersOf). Service accounts are not people and are left out.
func engineHolders(app *gocore.App) Holders {
	return func(ctx context.Context, permission string) ([]int, error) {
		engine, ok := app.Authorizer().(holderLister)
		if !ok {
			return nil, errNoEngine
		}

		subjects, err := engine.HoldersOf(ctx, permission)
		if err != nil {
			return nil, fmt.Errorf("find the holders: %w", err)
		}

		people := make([]int, 0, len(subjects))

		for _, subject := range subjects {
			if subject.Kind == authz.KindUser {
				people = append(people, subject.ID)
			}
		}

		return people, nil
	}
}
