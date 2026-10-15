package serviceaccount

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/event"
)

// The two conditions a key's owner is told about.
const (
	// ExpiryWarning is how long before a key expires its owner is told.
	ExpiryWarning = 14 * 24 * time.Hour
	// UnusedAfter is how long a key may go unused before its owner is told: 60 days since its last use, or
	// since its creation if it was never used.
	UnusedAfter = 60 * 24 * time.Hour
)

// NoticeKind names a condition a notice is sent for. A key is told about each condition at most once.
type NoticeKind string

const (
	// NoticeExpiring is the condition of a key that expires within ExpiryWarning.
	NoticeExpiring NoticeKind = "expiring"
	// NoticeUnused is the condition of an active key that has not been used for UnusedAfter.
	NoticeUnused NoticeKind = "unused"
)

// Notices are the conditions the key is in at the moment. A key that is revoked or expired is in none: it
// cannot be used, so there is nothing to warn about. Whether its account is active is the account's to say.
func (k Key) Notices(when time.Time) []NoticeKind {
	if !k.Usable(when) {
		return nil
	}

	var out []NoticeKind

	if k.ExpiresAt != nil && k.ExpiresAt.Sub(when) <= ExpiryWarning {
		out = append(out, NoticeExpiring)
	}

	since := k.CreatedAt
	if k.LastUsedAt != nil {
		since = *k.LastUsedAt
	}

	if when.Sub(since) >= UnusedAfter {
		out = append(out, NoticeUnused)
	}

	return out
}

// KeyWithAccount is a working key and the account it belongs to.
type KeyWithAccount struct {
	Key     Key
	Account Account
}

// NoticeStore is what the daily notice run needs of a Store: the working keys of active accounts, and the
// record that keeps each condition of each key to one notice.
type NoticeStore interface {
	// WorkingKeys returns the keys that work at the moment (not revoked, not expired) of accounts that are
	// active.
	WorkingKeys(ctx context.Context, at time.Time) ([]KeyWithAccount, error)
	// NoticesSent returns, by key id, the conditions the keys were already told about.
	NoticesSent(ctx context.Context, keyIDs []int) (map[int][]NoticeKind, error)
	// ClaimNotice records that the key is told about the condition. It returns false when it was already
	// recorded, so two runs never both tell.
	ClaimNotice(ctx context.Context, keyID int, kind NoticeKind, at time.Time) (bool, error)
}

// KeyNotice is the fact a notice announces: ids and facts only, the text is worded per recipient by
// whoever sends it (keynotice).
type KeyNotice struct {
	// OwnerID is the account's owner, who is told.
	OwnerID   int    `json:"owner_id"`   //nolint:tagliatelle // snake_case wire
	AccountID int    `json:"account_id"` //nolint:tagliatelle // snake_case wire
	Account   string `json:"account"`
	KeyID     int    `json:"key_id"` //nolint:tagliatelle // snake_case wire
	Key       string `json:"key"`
	// Kind is "expiring" or "unused".
	Kind NoticeKind `json:"kind"`
	// Date is the day the key expires (expiring) or was last used or created (unused), as YYYY-MM-DD.
	Date string `json:"date"`
	// Days is how many days are left before the key expires (expiring), or how long it went unused (unused).
	Days int `json:"days"`
}

// KeyNoticed is the event the daily run publishes for each notice, in the transaction that records it. The
// consumer is keynotice's; an application that wants its own can consume the event.
var KeyNoticed = event.Define[KeyNotice]( //nolint:gochecknoglobals // an event is declared once, as a value
	"serviceaccount.key-noticed")

// Notices tells the owner of each service account about its keys that expire soon or went unused, at most
// once per key and condition.
type Notices struct {
	store    NoticeStore
	transact Transactor
	log      *slog.Logger
	clock    Clock
}

// NewNotices returns the notice run.
func NewNotices(store NoticeStore, transact Transactor, log *slog.Logger, clock Clock) *Notices {
	return &Notices{store: store, transact: transact, log: log, clock: clock}
}

// Send publishes a notice for every key in a condition it was not yet told about, and returns how many it
// published. The claim and the event are one transaction, so nothing is half told, and a key whose notice
// fails is logged and left for the next run.
func (n *Notices) Send(ctx context.Context) (int, error) {
	now := n.clock.Now()

	working, err := n.store.WorkingKeys(ctx, now)
	if err != nil {
		return 0, apperr.Internal(err)
	}

	ids := make([]int, 0, len(working))
	for _, item := range working {
		ids = append(ids, item.Key.ID)
	}

	told, err := n.store.NoticesSent(ctx, ids)
	if err != nil {
		return 0, apperr.Internal(err)
	}

	sent := 0

	for _, item := range working {
		for _, kind := range item.Key.Notices(now) {
			if slices.Contains(told[item.Key.ID], kind) {
				continue
			}

			recorded, err := n.tell(ctx, item, kind, now)
			if err != nil {
				n.log.WarnContext(ctx, "service key notice not recorded", "key_id", item.Key.ID, "kind", kind, "error", err)

				continue
			}

			if recorded {
				sent++
			}
		}
	}

	return sent, nil
}

func (n *Notices) tell(ctx context.Context, item KeyWithAccount, kind NoticeKind, now time.Time) (bool, error) {
	recorded := false

	err := n.transact.WithinTransaction(ctx, func(txCtx context.Context) error {
		claimed, err := n.store.ClaimNotice(txCtx, item.Key.ID, kind, now)
		if err != nil {
			return fmt.Errorf("serviceaccount: claim the notice: %w", err)
		}

		if !claimed {
			return nil
		}

		err = KeyNoticed.Publish(txCtx, noticeOf(item, kind, now))
		if err != nil {
			return fmt.Errorf("publish the notice: %w", err)
		}

		recorded = true

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("serviceaccount: tell: %w", err)
	}

	return recorded, nil
}

// hoursPerDay turns the hours of a duration into the days a notice states.
const hoursPerDay = 24

func noticeOf(item KeyWithAccount, kind NoticeKind, now time.Time) KeyNotice {
	evt := KeyNotice{
		OwnerID: item.Account.OwnerID, AccountID: item.Account.ID, Account: item.Account.Name,
		KeyID: item.Key.ID, Key: item.Key.Name, Kind: kind, Date: "", Days: 0,
	}

	switch kind {
	case NoticeExpiring:
		if item.Key.ExpiresAt != nil {
			evt.Date = item.Key.ExpiresAt.Format(time.DateOnly)
			evt.Days = max(0, int(item.Key.ExpiresAt.Sub(now).Hours()/hoursPerDay))
		}
	case NoticeUnused:
		since := item.Key.CreatedAt
		if item.Key.LastUsedAt != nil {
			since = *item.Key.LastUsedAt
		}

		evt.Date = since.Format(time.DateOnly)
		evt.Days = int(now.Sub(since).Hours() / hoursPerDay)
	}

	return evt
}

// NoticeConsumer is the durable name of the consumer of KeyNoticed that tells the owner (keynotice.Install). The
// daily run sends notices only while a consumer of that name is installed, so a key's notice is never queued with
// nobody to read it.
const NoticeConsumer = "serviceaccount.key-notice"

// noticeInterval is how often the worker runs the notices: a day. A key is told about a condition once, whatever
// the number of runs, so a restart runs it again without telling anybody twice.
const noticeInterval = 24 * time.Hour

// NoticeWorker is the background work that runs the notices: once at start and then every day.
type NoticeWorker struct {
	notices   *Notices
	consumers func() []string
	log       *slog.Logger
}

// NewNoticeWorker returns the worker; consumers lists the durable names of the installed event consumers
// (gocore.App.Consumers). identity.WithServiceAccounts starts it.
func NewNoticeWorker(notices *Notices, consumers func() []string, log *slog.Logger) *NoticeWorker {
	return &NoticeWorker{notices: notices, consumers: consumers, log: log}
}

// Name identifies the worker in logs and metrics.
func (*NoticeWorker) Name() string { return "serviceaccount.key-notices" }

// Run sends the notices until ctx is cancelled.
func (w *NoticeWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(noticeInterval)
	defer ticker.Stop()

	for {
		w.once(ctx)

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (w *NoticeWorker) once(ctx context.Context) {
	if !slices.Contains(w.consumers(), NoticeConsumer) {
		w.log.DebugContext(ctx, "service key notices skipped: no consumer is installed (keynotice.Install)")

		return
	}

	sent, err := w.notices.Send(ctx)

	switch {
	case err != nil && ctx.Err() == nil:
		w.log.ErrorContext(ctx, "service key notices failed", "error", err)
	case sent > 0:
		w.log.InfoContext(ctx, "service key notices queued", "notices", sent)
	}
}
