package event

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Retention is how long an event that every consumer has finished stays in the
// outbox, with its consumers' attempts, before the housekeeper deletes it. Dead
// letters are never deleted by it: a dead letter's event is kept for as long as
// the letter is, because a retry needs it.
const Retention = 30 * 24 * time.Hour

const (
	housekeepingInterval = time.Hour
	pruneBatch           = 500
)

// Housekeeper keeps the queue's tables from growing for ever: it deletes the
// events processed more than Retention ago, and the attempts of each, in batches
// by primary key (portable SQL: MariaDB 10.3 has neither multi-table deletes with
// limits nor SKIP LOCKED). gocore's App.Events adds one to the background
// workers; there is nothing to configure.
type Housekeeper struct {
	db    *gorm.DB
	clock Clock
	log   *slog.Logger
}

// NewHousekeeper returns the housekeeper of the queue in db. A nil clock is the
// system clock, a nil log slog.Default.
func NewHousekeeper(db *gorm.DB, clock Clock, log *slog.Logger) *Housekeeper {
	if clock == nil {
		clock = systemClock{}
	}

	if log == nil {
		log = slog.Default()
	}

	return &Housekeeper{db: db, clock: clock, log: log}
}

// Name identifies the worker in logs.
func (h *Housekeeper) Name() string { return "event.housekeeping" }

// Run sweeps once at start and then every hour until ctx is cancelled.
func (h *Housekeeper) Run(ctx context.Context) error {
	ticker := time.NewTicker(housekeepingInterval)
	defer ticker.Stop()

	for {
		n, err := h.Sweep(ctx)

		switch {
		case err != nil && ctx.Err() == nil:
			h.log.ErrorContext(ctx, "event: housekeeping failed", "error", err)
		case n > 0:
			h.log.InfoContext(ctx, "event: deleted processed events", "events", n)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Sweep deletes the events processed more than Retention ago, with their
// consumers' attempts, and returns how many events it deleted. An event with a
// dead letter is kept.
func (h *Housekeeper) Sweep(ctx context.Context) (int, error) {
	cutoff := whole(h.clock.Now()).Add(-Retention)
	deleted := 0
	after := uint64(0)

	for {
		var ids []uint64

		// Dead letters' events are skipped here and again, under the lock, below.
		if err := h.db.WithContext(ctx).Table(outboxTable+" AS o").
			Where("o.id > ? AND o.processed_at IS NOT NULL AND o.processed_at < ?", after, cutoff).
			Where("NOT EXISTS (SELECT 1 FROM "+attemptsTable+" AS a WHERE a.event_id = o.id AND a.dead_at IS NOT NULL)").
			Order("o.id ASC").Limit(pruneBatch).
			Pluck("o.id", &ids).Error; err != nil {
			return deleted, fmt.Errorf("event housekeeping: select processed events: %w", err)
		}

		if len(ids) == 0 {
			return deleted, nil
		}

		n, err := h.prune(ctx, ids, cutoff)
		if err != nil {
			return deleted, err
		}

		deleted += n
		after = ids[len(ids)-1]
	}
}

// prune deletes the events among ids that are still processed and still without
// a dead letter. Their outbox rows are locked first, by primary key, as Retry
// locks them: a letter being retried keeps its event.
func (h *Housekeeper) prune(ctx context.Context, ids []uint64, cutoff time.Time) (int, error) {
	n := 0

	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked []uint64
		if err := tx.Table(outboxTable).
			Where("id IN ? AND processed_at IS NOT NULL AND processed_at < ?", ids, cutoff).
			Clauses(clause.Locking{Strength: "UPDATE"}).Pluck("id", &locked).Error; err != nil {
			return fmt.Errorf("lock events: %w", err)
		}

		if len(locked) == 0 {
			return nil
		}

		var dead []uint64
		if err := tx.Table(attemptsTable).Where("event_id IN ? AND dead_at IS NOT NULL", locked).Distinct().Pluck("event_id", &dead).Error; err != nil {
			return fmt.Errorf("find dead letters: %w", err)
		}

		doomed := make([]uint64, 0, len(locked))
		for _, id := range locked {
			if !slices.Contains(dead, id) {
				doomed = append(doomed, id)
			}
		}

		if len(doomed) == 0 {
			return nil
		}

		if err := tx.Table(attemptsTable).Where("event_id IN ?", doomed).Delete(&attemptRow{}).Error; err != nil {
			return fmt.Errorf("delete attempts: %w", err)
		}

		if err := tx.Table(outboxTable).Where("id IN ?", doomed).Delete(&OutboxEvent{}).Error; err != nil {
			return fmt.Errorf("delete events: %w", err)
		}

		n = len(doomed)

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("event housekeeping: %w", err)
	}

	return n, nil
}
