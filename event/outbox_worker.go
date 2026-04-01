package event

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// OutboxWorker publishes pending outbox events using the provided Bus.
// It polls the database at the configured interval and processes up to batchSize
// events per iteration.
type OutboxWorker struct {
	db           *gorm.DB
	bus          Bus
	log          *slog.Logger
	pollInterval time.Duration
	batchSize    int
}

func NewOutboxWorker(db *gorm.DB, bus Bus, log *slog.Logger, pollInterval time.Duration, batchSize int) *OutboxWorker {
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	if batchSize <= 0 {
		batchSize = 10
	}
	return &OutboxWorker{
		db:           db,
		bus:          bus,
		log:          log,
		pollInterval: pollInterval,
		batchSize:    batchSize,
	}
}

func (w *OutboxWorker) Name() string {
	return "outbox_worker"
}

func (w *OutboxWorker) Run(ctx context.Context) error {
	if w.db == nil || w.bus == nil {
		return nil
	}
	for {
		// process one batch
		pending, err := FetchPending(ctx, w.db, w.batchSize)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// sleep then retry
				time.Sleep(w.pollInterval)
				continue
			}
		}
		for _, ev := range pending {
			var env Envelope
			if err := json.Unmarshal(ev.Envelope, &env); err != nil {
				// skip malformed
				w.log.Error("outbox: malformed envelope", "err", err)
				continue
			}
			// attempt to publish
			if err := w.bus.Publish(ctx, env); err != nil {
				w.log.Error("outbox: publish failed",
					"event_id", ev.ID,
					"request_id", ev.RequestID,
					"err", err,
				)
				continue
			}
			// mark processed
			if err := w.markProcessed(ctx, ev.ID); err != nil {
				// Log but don't crash the loop — next poll will retry
				w.log.Error("outbox: mark processed", "err", err)
			}
		}

		// wait for next poll or context cancel
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.pollInterval):
		}
	}
}

func (w *OutboxWorker) markProcessed(ctx context.Context, id uint) error {
	tx := w.db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("outbox: begin tx: %w", tx.Error)
	}
	if err := MarkProcessed(ctx, tx, id); err != nil {
		tx.Rollback()
		return fmt.Errorf("outbox: mark processed: %w", err)
	}
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("outbox: commit: %w", err)
	}
	return nil
}
