package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wssto2/go-core/notification/push"
)

// Bundling (NOTIF-QUIET-001, rule 4; NOTIF-DELIVERY-001, rule 4): this many pushes due for one device at once become
// one push, or fewer when any of them waited for quiet hours to end, since two held pushes already buzz twice at seven.
const (
	bundleMinimum           = 3
	bundleMinimumAfterQuiet = 2
	bundleTitlesShown       = 3
)

// shouldBundle reports whether count pushes due together for one device are sent as one.
func shouldBundle(count int, anyHeld bool) bool {
	if anyHeld {
		return count >= bundleMinimumAfterQuiet
	}

	return count >= bundleMinimum
}

// bundleTag is the tag of a bundled push: a newer bundle replaces an older one on the device instead of stacking.
const bundleTag = "notification-bundle"

// pushPayload is the JSON the service worker reads. It carries no secrets and no customer contact data, only what it
// shows and needs to open the record (NOTIF-CONTENT-001). NotificationID is 0 for a bundle.
type pushPayload struct {
	NotificationID uint64 `json:"notification_id"` //nolint:tagliatelle // snake_case wire
	Category       string `json:"category"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	Link           string `json:"link"`
	// Tag groups notifications on the device: one tag per notification.
	Tag string `json:"tag"`
	// UnreadCount is for the badge on the app icon.
	UnreadCount int `json:"unread_count"` //nolint:tagliatelle // snake_case wire
}

// pushOutcome classifies a push service answer (NOTIF-DEVICE-001, rule 5).
type pushOutcome int

const (
	pushAccepted pushOutcome = iota // 2xx: the push service took the message
	pushGone                        // 404 or 410: the subscription no longer exists
	pushRetry                       // 429, 5xx, 401/403 (our VAPID credentials) or no answer: try again later
	pushRejected                    // any other 4xx, such as 413 or 400: a bug, retrying cannot help
)

// credentialsRejected reports whether the push service refused our VAPID credentials: a server configuration problem,
// never the device's. The pushes retry like an outage, so fixing the keys lets the queued ones go out.
func credentialsRejected(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

// classify maps an HTTP status to an outcome; status 0 means no answer arrived (network, timeout).
func classify(status int) pushOutcome {
	switch {
	case status >= http.StatusOK && status < http.StatusMultipleChoices:
		return pushAccepted
	case status == http.StatusNotFound || status == http.StatusGone:
		return pushGone
	case status == 0 || status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError || credentialsRejected(status):
		return pushRetry
	default:
		return pushRejected
	}
}

// blamesDevice reports whether a failed push counts towards disabling the device: a 4xx the push service gave the
// subscription. Not a success, not 404/410 (the device is removed at once), not 401/403 (our credentials), 413 (our
// payload) or 429 (the push service's rate limit), and not 5xx or no answer at all: those retry without blaming the
// device, so a push service outage or a wrong key pair disables nobody's devices.
func blamesDevice(status int) bool {
	switch {
	case status < http.StatusBadRequest || status >= http.StatusInternalServerError:
		return false
	case status == http.StatusNotFound, status == http.StatusGone, status == http.StatusRequestEntityTooLarge,
		status == http.StatusTooManyRequests, credentialsRejected(status):
		return false
	default:
		return true
	}
}

// recordPush notes one attempt: sent; retried with backoff, or longer when the push service said so, until the delivery
// expires; or failed at once when retrying cannot help.
func (d *deliveryRow) recordPush(status int, retryAfter time.Duration, errText string, now time.Time) pushOutcome {
	d.Attempts++
	d.UpdatedAt = now

	if status > 0 {
		s := int16(status) //nolint:gosec // an HTTP status
		d.LastStatus = &s
	}

	text := truncate(strings.TrimSpace(errText), lastErrorMax)
	d.LastError = &text

	outcome := classify(status)

	switch outcome {
	case pushAccepted:
		d.Status, d.SentAt, d.LastError = statusSent, &now, nil
	case pushRetry:
		next := now.Add(max(retryAfter, backoff(int(d.Attempts))))
		if !next.Before(d.ExpiresAt) {
			expired := truncate("expired: "+text, lastErrorMax)
			d.Status, d.LastError, d.failReason = statusFailed, &expired, reasonExpired

			break
		}

		d.Status, d.NextAttemptAt = statusPending, whole(next)
	case pushGone:
		d.Status, d.failReason = statusFailed, reasonGone
	case pushRejected:
		d.Status, d.failReason = statusFailed, reasonRejected
	}

	return outcome
}

// DeliverPushDue sends the pushes that are due, once, and returns how many deliveries it handled and the errors of
// those that could not be recorded; a push that failed is not an error here, it is retried or failed on its row
// (NOTIF-DELIVERY-001). The worker Install starts calls it every second when the application has WithPush; a test, or a
// command, calls it to deliver now. Like DeliverDue it claims what it sends with a lease, so a push goes out once
// however many instances run the worker, and it never touches an e-mail row.
//
// The pushes due for one device are handled together, so a burst can be bundled into one.
func (n *Notices) DeliverPushDue(ctx context.Context) (int, error) {
	if n.push == nil || n.store == nil {
		return 0, nil
	}

	now := n.clock.Now()
	leaseUntil := whole(now).Add(claimLease)

	due, err := n.store.claimDue(ctx, pushChannel, whole(now), leaseUntil, deliveryBatch)
	if err != nil {
		return 0, err
	}

	var (
		order    []uint64
		byDevice = map[uint64][]deliveryRow{}
		errs     []error
	)

	for _, d := range due {
		if _, seen := byDevice[d.DeviceID]; !seen {
			order = append(order, d.DeviceID)
		}

		byDevice[d.DeviceID] = append(byDevice[d.DeviceID], d)
	}

	for _, deviceID := range order {
		if ctx.Err() != nil || !canStartSend(leaseUntil, n.clock.Now()) {
			break // what is left is due again when the lease ends
		}

		err := n.deliverToDevice(ctx, deviceID, byDevice[deviceID])
		if err != nil {
			errs = append(errs, fmt.Errorf("device %d: %w", deviceID, err))
		}
	}

	return len(due), errors.Join(errs...)
}

// pending is a due push with its notification.
type pending struct {
	delivery deliveryRow
	note     row
}

// deliverToDevice sends what is due for one device: each push on its own, or one bundle for a burst. A delivery is
// cancelled instead when it must not go out: the device is gone, moved to another person or disabled, the notification
// is gone, or was read first (a held push goes out only if still unread, NOTIF-QUIET-001, rule 3).
func (n *Notices) deliverToDevice(ctx context.Context, deviceID uint64, items []deliveryRow) error {
	dev, found, err := n.store.deviceByID(ctx, deviceID)
	if err != nil {
		return err
	}

	if !found {
		return n.cancelAll(ctx, items, "device removed")
	}

	entries, anyHeld, err := n.triageAll(ctx, dev, items)
	if err != nil || len(entries) == 0 {
		return err
	}

	if shouldBundle(len(entries), anyHeld) {
		return n.sendPush(ctx, dev, entries, true)
	}

	for _, e := range entries {
		err := n.sendPush(ctx, dev, []pending{e}, false)
		if err != nil {
			return err
		}
	}

	return nil
}

// triageAll triages the due pushes of a device: the ones that can be sent, and whether any of them was held.
func (n *Notices) triageAll(ctx context.Context, dev deviceRow, items []deliveryRow) ([]pending, bool, error) {
	var (
		entries []pending
		anyHeld bool
	)

	for _, due := range items {
		entry, sendable, err := n.triage(ctx, dev, due)
		if err != nil {
			return nil, false, err
		}

		if sendable {
			anyHeld = anyHeld || due.Status == statusHeld

			entries = append(entries, entry)
		}
	}

	return entries, anyHeld, nil
}

// triage loads a due push's notification and cancels the push when it must not go out; it reports whether it can be
// sent.
func (n *Notices) triage(ctx context.Context, dev deviceRow, due deliveryRow) (pending, bool, error) {
	note, found, err := n.store.byID(ctx, due.NotificationID)
	if err != nil {
		return pending{}, false, err
	}

	reason := ""
	due.category = note.Category

	switch {
	case !found:
		reason = "notification deleted"
	case note.ReadAt != nil:
		reason = "read before delivery"
	case dev.UserID != note.UserID || dev.DisabledAt != nil:
		reason = "device no longer receives this person's pushes"
	}

	if reason != "" {
		return pending{}, false, n.finish(ctx, &due, func(due *deliveryRow) { due.cancel(reason, n.clock.Now()) })
	}

	return pending{delivery: due, note: note}, true, nil
}

func (n *Notices) cancelAll(ctx context.Context, items []deliveryRow, reason string) error {
	for _, d := range items {
		err := n.finish(ctx, &d, func(d *deliveryRow) { d.cancel(reason, n.clock.Now()) })
		if err != nil {
			return err
		}
	}

	return nil
}

// sendPush sends one push for the entries, a bundle when there are several, and records the same answer on each
// delivery and on the device (NOTIF-DEVICE-001, rule 5).
func (n *Notices) sendPush(ctx context.Context, dev deviceRow, entries []pending, bundle bool) error {
	unread, err := n.store.countUnread(ctx, int(dev.UserID))
	if err != nil {
		return fmt.Errorf("notification: count the unread for a push: %w", err)
	}

	now := n.clock.Now()
	first := entries[0].note

	payload := pushPayload{
		NotificationID: first.ID, Category: first.Category, Title: first.Title, Body: first.Body, Link: first.Link,
		Tag: "notification-" + strconv.FormatUint(first.ID, 10), UnreadCount: unread,
	}

	options := push.Options{TTL: entries[0].delivery.ExpiresAt.Sub(now), Urgency: push.UrgencyNormal}

	for _, e := range entries {
		options.TTL = min(options.TTL, e.delivery.ExpiresAt.Sub(now))

		if k, ok := n.kind(Kind{code: e.note.Category}); ok && k.high { //nolint:exhaustruct,exhaustruct_v5 // zero by design
			options.Urgency = push.UrgencyHigh
		}
	}

	if bundle {
		titles := make([]string, 0, len(entries))
		for _, e := range entries {
			titles = append(titles, e.note.Title)
		}

		title, body := n.bundleText(n.localeOf(ctx, int(dev.UserID)), len(entries), titles)
		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		payload = pushPayload{Title: title, Body: body, Link: "/", Tag: bundleTag, UnreadCount: unread}
	}

	result := n.transmit(ctx, dev, payload, options)
	now = n.clock.Now()

	var outcome pushOutcome

	for _, e := range entries {
		d := e.delivery
		d.category = e.note.Category
		outcome = d.recordPush(result.Status, result.RetryAfter, result.Detail, now)

		// Not written when the delivery was cancelled while the push was in flight: the cancellation stands.
		applied, err := n.store.saveDelivery(ctx, d)
		if err != nil {
			return err
		}

		if applied {
			n.noteEnd(ctx, d)
		}
	}

	if credentialsRejected(result.Status) {
		n.log.ErrorContext(
			ctx,
			"notification: the push service rejected the VAPID credentials: check the key pair and the subject",
			"status",
			result.Status,
			"detail",
			result.Detail,
		)
	}

	return n.noteOnDevice(ctx, dev, outcome, result.Status, now)
}

// transmit sends one encoded payload and turns every way it can fail into a Result the outcome rules classify: a
// subscription that cannot encrypt is gone (410), a payload too large is the 413 of a bug, no answer is status 0.
func (n *Notices) transmit(ctx context.Context, dev deviceRow, payload pushPayload, opts push.Options) push.Result {
	raw, err := json.Marshal(payload)
	if err != nil {
		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		return push.Result{Status: http.StatusRequestEntityTooLarge, Detail: err.Error()}
	}

	res, err := n.push.Send(ctx, dev.subscription(), raw, opts)

	switch {
	case errors.Is(err, push.ErrSubscriptionUnusable):
		return push.Result{Status: http.StatusGone, Detail: err.Error()} //nolint:exhaustruct,exhaustruct_v5 // zero by design
	case errors.Is(err, push.ErrPayloadTooLarge):
		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		return push.Result{Status: http.StatusRequestEntityTooLarge, Detail: err.Error()}
	case err != nil:
		return push.Result{Detail: err.Error()} //nolint:exhaustruct,exhaustruct_v5 // zero by design
	}

	return res
}

// noteOnDevice applies NOTIF-DEVICE-001, rule 5: gone removes the device at once; a failure that is the device's fault
// counts towards disabling it, one that is ours or the push service's does not.
func (n *Notices) noteOnDevice(
	ctx context.Context,
	dev deviceRow,
	outcome pushOutcome,
	status int,
	now time.Time,
) error {
	switch outcome {
	case pushAccepted:
		return n.store.pushSucceeded(ctx, dev.ID, now)
	case pushGone:
		return n.store.dropGoneDevice(ctx, dev.ID, dev.EndpointHash, now)
	case pushRetry, pushRejected:
		if !blamesDevice(status) {
			return nil
		}

		disabled, err := n.store.pushFailed(ctx, dev.ID, now)
		if err != nil || !disabled {
			return err
		}

		return n.store.cancelPushOf(ctx, dev.ID, "device disabled after repeated failures", now)
	}

	return nil
}

// localeOf is the language of a person, for the text of a bundle; empty when they cannot be found.
func (n *Notices) localeOf(ctx context.Context, userID int) string {
	people, err := n.people.Find(ctx, []int{userID})
	if err != nil || len(people) == 0 {
		return ""
	}

	return people[0].Locale
}

// pushWorker sends the due pushes (NOTIF-DELIVERY-001), like deliveryWorker does e-mail.
type pushWorker struct{ notices *Notices }

// Name identifies the worker in logs and metrics.
func (w *pushWorker) Name() string { return "notification.push" }

// Run delivers until ctx is cancelled.
func (w *pushWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		handled, err := w.notices.DeliverPushDue(ctx)
		if err != nil && ctx.Err() == nil {
			w.notices.log.WarnContext(ctx, "notification: push delivery errors", "handled", handled, "error", err)
		}

		if handled >= deliveryBatch && err == nil && ctx.Err() == nil {
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
