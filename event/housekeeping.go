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
// letters are not deleted by it unless the housekeeper was told how long to keep
// them (KeepDeadLetters): a dead letter's event is kept for as long as the
// letter is, because a retry needs it.
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
	// deadFor is how long a dead letter is kept; zero is for ever.
	deadFor time.Duration
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

// KeepDeadLetters makes the housekeeper delete dead letters, and the events only they kept, d after the consumer gave
// up on them, instead of keeping them for ever: a letter nobody retried in that time is not going to be. Zero or less
// keeps them for ever, which is the default. An application built on gocore gives gocore.New the option
// WithDeadLetterRetention, which sets this on the housekeeper App.Events adds; one that builds its own calls it:
//
//	event.NewHousekeeper(db, clock, log).KeepDeadLetters(90 * 24 * time.Hour)
//
// It returns the housekeeper, so it chains.
func (h *Housekeeper) KeepDeadLetters(d time.Duration) *Housekeeper {
	h.deadFor = max(d, 0)

	return h
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
// dead letter is kept, unless KeepDeadLetters ended the letter: the letters older
// than that go first, and their events follow when nothing else keeps them.
func (h *Housekeeper) Sweep(ctx context.Context) (int, error) {
	err := h.expireDeadLetters(ctx)
	if err != nil {
		return 0, err
	}

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

// expireDeadLetters deletes the dead letters older than the time KeepDeadLetters set, in batches of events. The outbox
// rows of a batch are locked first, by primary key, as Retry locks them: a letter being retried is no longer dead when
// the delete looks at it, and stays.
func (h *Housekeeper) expireDeadLetters(ctx context.Context) error {
	if h.deadFor <= 0 {
		return nil
	}

	cutoff := whole(h.clock.Now()).Add(-h.deadFor)
	after := uint64(0)
	letters := 0

	for {
		var ids []uint64

		err := h.db.WithContext(ctx).Table(attemptsTable).
			Where("event_id > ? AND dead_at IS NOT NULL AND dead_at < ?", after, cutoff).
			Distinct().Order("event_id ASC").Limit(pruneBatch).Pluck("event_id", &ids).Error
		if err != nil {
			return fmt.Errorf("event housekeeping: select dead letters: %w", err)
		}

		if len(ids) == 0 {
			break
		}

		n, err := h.deleteLetters(ctx, ids, cutoff)
		if err != nil {
			return err
		}

		letters += n
		after = ids[len(ids)-1]
	}

	if letters > 0 {
		h.log.InfoContext(ctx, "event: deleted dead letters", "letters", letters)
	}

	return nil
}

func (h *Housekeeper) deleteLetters(ctx context.Context, ids []uint64, cutoff time.Time) (int, error) {
	deleted := 0

	err := h.db.WithContext(ctx).Transaction(func(conn *gorm.DB) error {
		var locked []uint64
		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		err := conn.Table(outboxTable).Where("id IN ?", ids).
			Clauses(clause.Locking{Strength: "UPDATE"}).Pluck("id", &locked).Error
		if err != nil {
			return fmt.Errorf("lock events: %w", err)
		}

		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		res := conn.Table(attemptsTable).
			Where("event_id IN ? AND dead_at IS NOT NULL AND dead_at < ?", ids, cutoff).Delete(&attemptRow{})
		deleted = int(res.RowsAffected)

		return res.Error
	})
	if err != nil {
		return 0, fmt.Errorf("event housekeeping: delete dead letters: %w", err)
	}

	return deleted, nil
}
