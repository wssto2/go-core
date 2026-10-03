package event

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Errors of the dead-letter service.
var (
	// ErrDeadLetterNotFound: the consumer has no dead letter for the event, for
	// instance because it was retried meanwhile.
	ErrDeadLetterNotFound = errors.New("dead letter not found")
	// ErrDeadLetterEventGone: the event itself was pruned from the outbox, so
	// there is nothing left to handle again.
	ErrDeadLetterEventGone = errors.New("the dead letter's event was pruned from the outbox")
)

// DeadLetter is an event a consumer gave up on: its handler failed every
// attempt, or the event could never be read.
type DeadLetter struct {
	EventID  uint64
	Consumer string
	// EventName and EventCreatedAt are empty when the outbox row was pruned;
	// such a letter cannot be retried.
	EventName      string
	EventCreatedAt *time.Time
	Attempts       int
	LastError      string
	DeadAt         time.Time
}

// Retryable reports whether the event is still there to be handled again.
func (d DeadLetter) Retryable() bool { return d.EventName != "" }

// DeadLetters lists the events consumers gave up on and puts them back in the
// queue. HTTP routes for it come with the notification module.
type DeadLetters struct {
	db    *gorm.DB
	clock Clock
}

// NewDeadLetters returns the dead-letter service over db. A nil clock is the
// system clock.
func NewDeadLetters(db *gorm.DB, clock Clock) *DeadLetters {
	if clock == nil {
		clock = systemClock{}
	}

	return &DeadLetters{db: db, clock: clock}
}

// DeadLetterQuery selects a page of dead letters, newest first.
type DeadLetterQuery struct {
	// Consumer limits the list to one consumer; empty lists all.
	Consumer string
	// Limit is the page size (default 25, at most 100); Offset the rows skipped.
	Limit, Offset int
}

type deadLetterRow struct {
	EventID        uint64     `gorm:"column:event_id"`
	Consumer       string     `gorm:"column:consumer"`
	EventName      *string    `gorm:"column:event_name"`
	EventCreatedAt *time.Time `gorm:"column:event_created_at"`
	Attempts       int        `gorm:"column:attempts"`
	LastError      string     `gorm:"column:last_error"`
	DeadAt         time.Time  `gorm:"column:dead_at"`
}

// List returns a page of dead letters and how many there are in all. The outbox
// row is joined loosely, so a letter whose event was pruned is still listed.
func (d *DeadLetters) List(ctx context.Context, q DeadLetterQuery) ([]DeadLetter, int, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 25
	}

	limit = min(limit, 100)

	query := func() *gorm.DB {
		tx := d.db.WithContext(ctx).Table(attemptsTable + " AS a").
			Joins("LEFT JOIN " + outboxTable + " AS o ON o.id = a.event_id").
			Where("a.dead_at IS NOT NULL")
		if q.Consumer != "" {
			tx = tx.Where("a.consumer = ?", q.Consumer)
		}

		return tx
	}

	var total int64
	if err := query().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count dead letters: %w", err)
	}

	var rows []deadLetterRow
	if err := query().
		Select("a.event_id, a.consumer, o.event_type AS event_name, o.created_at AS event_created_at, a.attempts, a.last_error, a.dead_at").
		Order("a.dead_at DESC, a.event_id DESC, a.consumer ASC").
		Limit(limit).Offset(max(q.Offset, 0)).
		Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list dead letters: %w", err)
	}

	out := make([]DeadLetter, 0, len(rows))

	for _, r := range rows {
		letter := DeadLetter{EventID: r.EventID, Consumer: r.Consumer, EventCreatedAt: r.EventCreatedAt, Attempts: r.Attempts, LastError: r.LastError, DeadAt: r.DeadAt}
		if r.EventName != nil {
			letter.EventName = *r.EventName
		}

		out = append(out, letter)
	}

	return out, int(total), nil
}

// Retry puts one consumer's dead letter back in the queue: its attempts start
// again and the consumer claims the event on its next poll.
func (d *DeadLetters) Retry(ctx context.Context, eventID uint64, consumer string) error {
	now := whole(d.clock.Now())

	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var found []uint64
		if err := tx.Table(outboxTable).Where("id = ?", eventID).
			Clauses(clause.Locking{Strength: "UPDATE"}).Pluck("id", &found).Error; err != nil {
			return err
		}

		if len(found) == 0 {
			return ErrDeadLetterEventGone
		}

		// A changed row is counted even where matched rows are not (no clientFoundRows):
		// dead_at goes from set to NULL.
		revived := tx.Table(attemptsTable).
			Where("event_id = ? AND consumer = ? AND dead_at IS NOT NULL", eventID, consumer).
			Updates(map[string]any{"dead_at": nil, "attempts": 0, "next_attempt_at": now, "updated_at": now})
		if revived.Error != nil {
			return revived.Error
		}

		if revived.RowsAffected == 0 {
			return ErrDeadLetterNotFound
		}

		return tx.Table(outboxTable).Where("id = ?", eventID).Update("processed_at", nil).Error
	})
	if err != nil {
		return fmt.Errorf("retry dead letter %d of %q: %w", eventID, consumer, err)
	}

	return nil
}

// RetryAll puts every dead letter of the consumer whose event is still there
// back in the queue, and returns how many.
func (d *DeadLetters) RetryAll(ctx context.Context, consumer string) (int, error) {
	var ids []uint64
	if err := d.db.WithContext(ctx).Table(attemptsTable).
		Where("consumer = ? AND dead_at IS NOT NULL", consumer).Order("event_id ASC").
		Pluck("event_id", &ids).Error; err != nil {
		return 0, fmt.Errorf("list dead letters of %q: %w", consumer, err)
	}

	retried := 0

	for _, id := range ids {
		err := d.Retry(ctx, id, consumer)

		switch {
		case err == nil:
			retried++
		case errors.Is(err, ErrDeadLetterNotFound), errors.Is(err, ErrDeadLetterEventGone):
			// retried or pruned meanwhile
		default:
			return retried, err
		}
	}

	return retried, nil
}
