package event

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
)

// PublishFunc delivers a serialised event envelope to the given subject.
// For NATS, wrap natsClient.Publish. The data is the raw Envelope JSON.
type PublishFunc func(ctx context.Context, subject string, data []byte) error

// OutboxWorker polls the outbox table and delivers pending events via PublishFunc.
// It processes up to batchSize events per iteration and sleeps pollInterval between batches.
type OutboxWorker struct {
	db           *gorm.DB
	publish      PublishFunc
	log          *slog.Logger
	pollInterval time.Duration
	batchSize    int
}

func NewOutboxWorker(db *gorm.DB, publish PublishFunc, log *slog.Logger, pollInterval time.Duration, batchSize int) *OutboxWorker {
	if log == nil {
		log = slog.Default()
	}
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	if batchSize <= 0 {
		batchSize = 10
	}
	return &OutboxWorker{
		db:           db,
		publish:      publish,
		log:          log,
		pollInterval: pollInterval,
		batchSize:    batchSize,
	}
}

func (w *OutboxWorker) Name() string {
	return "outbox_worker"
}

func (w *OutboxWorker) Run(ctx context.Context) error {
	if w.db == nil || w.publish == nil {
		return nil
	}
	for {
		pending, err := FetchPending(ctx, w.db, w.batchSize)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				time.Sleep(w.pollInterval)
				continue
			}
		}
		for _, ev := range pending {
			if ev.EventType == "" {
				w.log.Error("outbox: dead-lettering event with empty type; marking processed to prevent infinite retry", "event_id", ev.ID)
				if err := w.markProcessed(ctx, ev.ID); err != nil {
					w.log.Error("outbox: failed to mark empty-type event as processed", "event_id", ev.ID, "err", err)
				}
				continue
			}
			if err := w.publish(ctx, ev.EventType, ev.Envelope); err != nil {
				w.log.Error("outbox: publish failed",
					"event_id", ev.ID,
					"event_type", ev.EventType,
					"request_id", ev.RequestID,
					"err", err,
				)
				continue
			}
			if err := w.markProcessed(ctx, ev.ID); err != nil {
				w.log.Error("outbox: mark processed", "event_id", ev.ID, "err", err)
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.pollInterval):
		}
	}
}

func (w *OutboxWorker) markProcessed(ctx context.Context, id uint) error {
	return database.NewTransactor(w.db).WithinTransaction(ctx, func(ctx context.Context) error {
		tx, ok := database.TxFromContext(ctx)
		if !ok {
			return fmt.Errorf("outbox: no transaction in context")
		}
		return MarkProcessed(ctx, tx, id)
	})
}

