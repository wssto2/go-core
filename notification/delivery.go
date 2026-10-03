package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/mail"
	"github.com/wssto2/go-core/notification/mailtext"
)

// How a delivery is retried (NOTIF-DELIVERY-001): the first retry waits RetryBaseDelay, each next one twice as
// long up to RetryMaxDelay, for as long as DeliveryTTL lasts from the moment the delivery may first be sent. After
// that, or on a permanent error, the delivery is failed.
const (
	// DeliveryTTL is how long a failing e-mail is retried.
	DeliveryTTL = 24 * time.Hour
	// RetryBaseDelay is the wait before the first retry; it doubles each time.
	RetryBaseDelay = 30 * time.Second
	// RetryMaxDelay is the longest wait between two attempts.
	RetryMaxDelay = time.Hour
)

const (
	// claimLease is how long a worker owns the deliveries it claimed (NOTIF-DELIVERY-001, rule 4).
	claimLease = 5 * time.Minute
	// sendMargin is the least of the lease that must be left to start a send: more than one send can take, so no
	// send is still running when the lease ends.
	sendMargin    = time.Minute
	deliveryBatch = 100
	pollInterval  = time.Second
	lastErrorMax  = 500
)

// The statuses of a delivery (NOTIF-DELIVERY-001, rule 1).
const (
	statusPending   = "pending"
	statusHeld      = "held" // waits for the end of the person's quiet hours
	statusSent      = "sent"
	statusFailed    = "failed"
	statusCancelled = "cancelled"
)

// dueStatuses are the statuses the worker still has to send.
var dueStatuses = []string{statusPending, statusHeld}

// newEmailDelivery is the delivery of an e-mail made at now, to go out at release or, when that is not after now,
// at once. A held delivery's lifetime starts at the release, so a night of quiet hours does not use up the TTL.
// The notification id is set when the notification is written.
func newEmailDelivery(address string, now, release time.Time) deliveryRow {
	d := deliveryRow{
		Address: &address, Channel: emailChannel, Status: statusPending,
		NextAttemptAt: now, ExpiresAt: now.Add(DeliveryTTL), CreatedAt: now, UpdatedAt: now,
	}

	if release.After(now) {
		d.Status, d.NextAttemptAt, d.ExpiresAt = statusHeld, whole(release), whole(release).Add(DeliveryTTL)
	}

	return d
}

// record notes one attempt: sent; failed at once on a permanent error; or retried with backoff until
// the delivery expires.
func (d *deliveryRow) record(sendErr error, now time.Time) {
	d.Attempts++
	d.UpdatedAt = now

	if sendErr == nil {
		d.Status, d.SentAt, d.LastError = statusSent, &now, nil

		return
	}

	failed := func(reason string) {
		d.Status = statusFailed
		msg := truncate(reason+sendErr.Error(), lastErrorMax)
		d.LastError = &msg
	}

	if errors.Is(sendErr, mail.ErrPermanent) {
		failed("permanent: ")

		return
	}

	next := now.Add(backoff(int(d.Attempts)))
	if !next.Before(d.ExpiresAt) {
		failed("expired: ")

		return
	}

	msg := truncate(sendErr.Error(), lastErrorMax)
	d.Status, d.NextAttemptAt, d.LastError = statusPending, next, &msg
}

// cancel ends a delivery that must not be sent any more.
func (d *deliveryRow) cancel(reason string, now time.Time) {
	d.Status, d.UpdatedAt = statusCancelled, now
	d.LastError = &reason
}

// backoff doubles from RetryBaseDelay per attempt, capped at RetryMaxDelay.
func backoff(attempts int) time.Duration {
	delay := RetryBaseDelay

	for i := 1; i < attempts && delay < RetryMaxDelay; i++ {
		delay *= 2
	}

	return min(delay, RetryMaxDelay)
}

// canStartSend reports whether a worker whose lease ends at leaseUntil may still start a send at now.
func canStartSend(leaseUntil, now time.Time) bool { return now.Add(sendMargin).Before(leaseUntil) }

// emailPlans decides who is e-mailed for a notification of category k and when (NOTIF-PREF-001, NOTIF-QUIET-001):
// the people whose resolved e-mail setting is on and who have an address, each with the delivery that goes out
// at once or, in their quiet hours, when those end. Nothing is planned without mail, or for an internal category.
func (n *Notices) emailPlans(ctx context.Context, k Kind, people []account.Account, now time.Time) (map[uint32]deliveryRow, error) {
	if n.mail == nil || k.internal {
		return nil, nil
	}

	var ids []int

	for _, a := range people {
		if strings.TrimSpace(a.Email) != "" {
			ids = append(ids, a.ID)
		}
	}

	if len(ids) == 0 {
		return nil, nil
	}

	settings, err := n.Settings.emailFor(ctx, k, ids)
	if err != nil {
		return nil, err
	}

	quiet, err := n.store.quietOf(ctx, ids)
	if err != nil {
		return nil, err
	}

	plans := map[uint32]deliveryRow{}

	for _, a := range people {
		if !settings[a.ID].Enabled {
			continue
		}

		q, ok := quiet[a.ID]
		if !ok {
			q = DefaultQuietHours()
		}

		plans[uint32(a.ID)] = newEmailDelivery(strings.TrimSpace(a.Email), now, q.ReleaseAt(now, n.location)) //nolint:gosec // resolve keeps ids between 1 and MaxInt32
	}

	return plans, nil
}

// DeliverDue sends the e-mails that are due, once, and returns how many deliveries it handled and the errors of those
// that failed to be recorded; a failed send is not an error here, it is retried or failed on its row (NOTIF-DELIVERY-001).
// The worker Install starts calls it every second; a test, or a command, calls it to deliver now.
//
// A delivery is sent once however many instances run the worker: due rows are claimed with a lease, and no send starts
// in the last minute of it.
func (n *Notices) DeliverDue(ctx context.Context) (int, error) {
	if n.mail == nil || n.store == nil {
		return 0, nil
	}

	now := n.clock.Now()
	leaseUntil := whole(now).Add(claimLease)

	due, err := n.store.claimDue(ctx, whole(now), leaseUntil, deliveryBatch)
	if err != nil {
		return 0, err
	}

	var errs []error

	for _, d := range due {
		if ctx.Err() != nil || !canStartSend(leaseUntil, n.clock.Now()) {
			break // what is left is due again when the lease ends
		}

		if err := n.deliver(ctx, d); err != nil {
			errs = append(errs, fmt.Errorf("delivery %d: %w", d.ID, err))
		}
	}

	return len(due), errors.Join(errs...)
}

// deliver sends one claimed delivery, or cancels it when it must not go out: the notification is gone, or was read
// before it was sent (a held e-mail goes out only if still unread, NOTIF-QUIET-001, rule 3).
func (n *Notices) deliver(ctx context.Context, d deliveryRow) error {
	note, found, err := n.store.byID(ctx, d.NotificationID)
	if err != nil {
		return err
	}

	switch {
	case !found:
		return n.finish(ctx, &d, func(d *deliveryRow) { d.cancel("notification deleted", n.clock.Now()) })
	case note.ReadAt != nil:
		return n.finish(ctx, &d, func(d *deliveryRow) { d.cancel("read before delivery", n.clock.Now()) })
	}

	people, err := n.people.Find(ctx, []int{int(note.UserID)}) //nolint:gosec // an id of this table's own column
	if err != nil {
		return fmt.Errorf("look up the recipient: %w", err)
	}

	var to account.Account
	if len(people) > 0 {
		to = people[0]
	}

	sendErr := n.sendEmail(ctx, to, *d.Address, note)

	return n.finish(ctx, &d, func(d *deliveryRow) { d.record(sendErr, n.clock.Now()) })
}

// finish applies the outcome and saves it, unless the delivery was ended meanwhile, which then stands.
func (n *Notices) finish(ctx context.Context, d *deliveryRow, outcome func(*deliveryRow)) error {
	outcome(d)

	if d.Status == statusFailed {
		n.log.WarnContext(ctx, "notification: an e-mail was given up on", "delivery", d.ID, "notification", d.NotificationID, "error", deref(d.LastError))
	} else {
		n.log.DebugContext(ctx, "notification: e-mail delivery", "delivery", d.ID, "status", d.Status, "attempts", d.Attempts)
	}

	return n.store.saveDelivery(ctx, *d)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// sendEmail renders a notification in the person's language and sends it. The subject is the title on one line, the body
// the notification's body and the link that opens it (AppURL plus the in-app link), and a footer that says why the person
// gets it and where to change it.
func (n *Notices) sendEmail(ctx context.Context, to account.Account, address string, note row) error {
	name := to.Name
	if name == "" {
		name = to.Login
	}

	data := mailtext.EmailData{Name: name, Title: strings.Join(strings.Fields(note.Title), " "), Body: note.Body}
	if note.Link != "" {
		data.OpenURL = n.appURL + note.Link
	}

	content, err := mail.Fallback(n.mail.Renderer, mailtext.Defaults).Render(ctx, to.Locale, mailtext.Email, data)
	if err != nil {
		return fmt.Errorf("render the e-mail: %w", err)
	}

	return n.mail.Sender.Send(ctx, content.To(address))
}
