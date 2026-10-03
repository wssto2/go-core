package event

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The queue's two tables. outbox_events is OutboxEvent; every consumer's state
// for one event is a row of event_consumer_attempts, so each consumer claims,
// retries and gives up on its own and one failing consumer holds back no other.
const (
	outboxTable   = "outbox_events"
	attemptsTable = "event_consumer_attempts"
)

// Tunables of the queue. The lease is how long a worker owns the events it is
// handling: one that dies leaves them due again when it ends.
const (
	batchSize     = 50
	claimLease    = 5 * time.Minute
	pollInterval  = time.Second
	maxErrorRunes = 500
)

// Clock tells the time; gocore.Clock satisfies it.
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// whole is a time as the DATETIME columns keep it: to the second.
func whole(t time.Time) time.Time { return t.Truncate(time.Second) }

// attemptRow is an event_consumer_attempts row: one consumer's state for one
// event. It exists while the event is leased or being retried, and after it
// finished: done (DoneAt) or given up on (DeadAt, the dead letter).
type attemptRow struct {
	EventID       uint64     `gorm:"column:event_id;primaryKey;autoIncrement:false;type:bigint unsigned;not null"`
	Consumer      string     `gorm:"column:consumer;primaryKey;size:100;not null"`
	Attempts      int        `gorm:"column:attempts;type:smallint unsigned;not null;default:0"`
	NextAttemptAt time.Time  `gorm:"column:next_attempt_at;type:datetime;not null"`
	LastError     string     `gorm:"column:last_error;size:500;not null;default:''"`
	DoneAt        *time.Time `gorm:"column:done_at;type:datetime"`
	DeadAt        *time.Time `gorm:"column:dead_at;type:datetime;index:event_consumer_attempts_dead"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;type:datetime;not null;autoUpdateTime:false"`
}

func (attemptRow) TableName() string { return attemptsTable }

// Migrate creates both tables from their GORM models, for tests on SQLite and
// as the reference the migration files are compared with (event/migrations).
func Migrate(db *gorm.DB) error {
	if err := EnsureOutboxSchema(db); err != nil {
		return err
	}

	return db.AutoMigrate(&attemptRow{})
}

// claimedEvent is a due event leased to one consumer.
type claimedEvent struct {
	ID        uint64
	EventType string
	Envelope  string // text, whatever the driver returns the JSON column as
	Attempts  int
}

// claim returns the events of eventName that are due for consumer (not done,
// not dead, not leased and not waiting for a retry) and leases them until
// leaseUntil, in portable SQL (MariaDB 10.3 has neither SKIP LOCKED nor FOR
// UPDATE OF):
//
//  1. Outside the transaction, a plain read picks the candidates.
//  2. The transaction's first statement locks the candidates' outbox rows
//     alone, by primary key. A worker claiming the same events waits here
//     until this one commits.
//  3. A plain read of the consumer's attempts rows. It is the transaction's
//     first non-locking read, so its snapshot is taken now, with the outbox
//     rows held: it sees a lease another worker committed before we got the
//     locks, and those events are dropped. It is deliberately not a locking
//     read: locking rows that do not exist yet takes gap locks, and two
//     workers inserting their leases into one gap would deadlock. Nothing else
//     inserts a missing row for this consumer meanwhile: a claim needs the
//     outbox row lock we hold.
//  4. The leases are written (next_attempt_at = leaseUntil) and committed.
//
// A worker whose lease ran out while it was still handling an event can meet
// a second one taking it: delivery is at least once.
func claim(ctx context.Context, db *gorm.DB, consumer, eventName string, now, leaseUntil time.Time, limit int) ([]claimedEvent, error) {
	var candidates []uint64

	if err := db.WithContext(ctx).Table(outboxTable+" AS o").
		Joins("LEFT JOIN "+attemptsTable+" AS a ON a.event_id = o.id AND a.consumer = ?", consumer).
		Where("o.processed_at IS NULL AND o.event_type = ?", eventName).
		Where("(a.event_id IS NULL OR (a.done_at IS NULL AND a.dead_at IS NULL AND a.next_attempt_at <= ?))", now).
		Order("o.id ASC").
		Limit(limit).
		Pluck("o.id", &candidates).Error; err != nil {
		return nil, fmt.Errorf("select due events: %w", err)
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	var claimed []claimedEvent

	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked []claimedEvent

		if err := tx.Table(outboxTable).
			Select("id, event_type, envelope").
			Where("id IN ? AND processed_at IS NULL", candidates).
			Order("id ASC").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Scan(&locked).Error; err != nil {
			return fmt.Errorf("lock due events: %w", err)
		}

		if len(locked) == 0 {
			return nil
		}

		ids := make([]uint64, 0, len(locked))
		for _, c := range locked {
			ids = append(ids, c.ID)
		}

		var current []attemptRow
		if err := tx.Where("consumer = ? AND event_id IN ?", consumer, ids).Find(&current).Error; err != nil {
			return fmt.Errorf("read attempts: %w", err)
		}

		byEvent := make(map[uint64]attemptRow, len(current))
		for _, a := range current {
			byEvent[a.EventID] = a
		}

		leases := make([]attemptRow, 0, len(locked))

		for _, c := range locked {
			a, ok := byEvent[c.ID]
			if ok && (a.DoneAt != nil || a.DeadAt != nil || a.NextAttemptAt.After(now)) {
				continue // leased by another worker meanwhile, or finished
			}

			c.Attempts = a.Attempts
			claimed = append(claimed, c)
			leases = append(leases, attemptRow{EventID: c.ID, Consumer: consumer, Attempts: c.Attempts, NextAttemptAt: leaseUntil, UpdatedAt: now})
		}

		if len(leases) == 0 {
			return nil
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "event_id"}, {Name: "consumer"}},
			DoUpdates: clause.AssignmentColumns([]string{"next_attempt_at", "updated_at"}),
		}).Create(&leases).Error; err != nil {
			return fmt.Errorf("lease due events: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claim events for %q: %w", consumer, err)
	}

	return claimed, nil
}

// finish records the outcome of one consumer's handling of an event in one
// transaction and, when every consumer of the event has finished (done or dead),
// marks the outbox row processed so it can be pruned. The outbox row is locked
// first, by primary key: the finishing consumers of one event queue up here, so
// the last one to commit sees all the others.
func finish(ctx context.Context, db *gorm.DB, row attemptRow, siblings []string) error {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked []uint64
		if err := tx.Table(outboxTable).Where("id = ?", row.EventID).
			Clauses(clause.Locking{Strength: "UPDATE"}).Pluck("id", &locked).Error; err != nil {
			return fmt.Errorf("lock the event: %w", err)
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "event_id"}, {Name: "consumer"}},
			DoUpdates: clause.AssignmentColumns([]string{"attempts", "next_attempt_at", "last_error", "done_at", "dead_at", "updated_at"}),
		}).Create(&row).Error; err != nil {
			return fmt.Errorf("save the outcome: %w", err)
		}

		if len(locked) == 0 || (row.DoneAt == nil && row.DeadAt == nil) {
			return nil
		}

		var finished int64
		if err := tx.Table(attemptsTable).
			Where("event_id = ? AND consumer IN ? AND (done_at IS NOT NULL OR dead_at IS NOT NULL)", row.EventID, siblings).
			Count(&finished).Error; err != nil {
			return fmt.Errorf("count finished consumers: %w", err)
		}

		if int(finished) < len(siblings) {
			return nil
		}

		return tx.Table(outboxTable).Where("id = ?", row.EventID).Update("processed_at", row.UpdatedAt).Error
	})
	if err != nil {
		return fmt.Errorf("record event %d for %q: %w", row.EventID, row.Consumer, err)
	}

	return nil
}

func truncateError(s string) string {
	if r := []rune(s); len(r) > maxErrorRunes {
		return string(r[:maxErrorRunes])
	}

	return s
}
