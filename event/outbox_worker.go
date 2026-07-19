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

// WorkerOption configures an OutboxWorker.
type WorkerOption func(*OutboxWorker)

// WithExitWhenIdle makes Run return nil when there are no pending events,
// instead of sleeping and polling again. Use this for cron-driven one-shot runs.
func WithExitWhenIdle() WorkerOption {
	return func(w *OutboxWorker) { w.exitWhenIdle = true }
}

// OutboxWorker polls the outbox table and delivers pending events via PublishFunc.
// It processes up to batchSize events per iteration and sleeps pollInterval between batches.
type OutboxWorker struct {
	db           *gorm.DB
	publish      PublishFunc
	log          *slog.Logger
	pollInterval time.Duration
	batchSize    int
	exitWhenIdle bool
}

// What it is: the transactional outbox pattern. The problem it solves is
// subtle and it's the one people miss:
// // BROKEN — the classic dual-write bug:
// tx.Commit()                    // vehicle marked sold ✓
// publishEvent("vehicle.sold")   // ← process crashes HERE → event lost forever
//
//	//   or: publish succeeds but tx rolled back → phantom event
//
// The outbox fixes it by making the event part of the transaction:
//
//	s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
//	    repo.Update(txCtx, &vehicle)                          // mark sold
//	    outbox.Write(txCtx, VehicleSoldEvent{ID: vehicle.ID}) // same tx, same commit
//	    return nil
//	})
//
// Later, NewOutboxWorker polls the outbox table (pollInterval, batchSize)
// and publishes rows that committed. Crash-safe: unpublished rows are
// still there after restart.
//
// Real use case from your domain: a vehicle is sold → you must de-list it from
// three external portals. If you call the portal APIs inline, the sale request
// is slow and fails when a portal is down. If you fire-and-forget a goroutine,
// a deploy at the wrong moment silently leaves a sold car advertised.
// Outbox: the "sold" event commits atomically with the sale, and the worker
// retries portal de-listing until it succeeds.
//
// Litmus test: "if the server restarts at the worst possible moment, is it
// a bug for this side effect to be lost or to fire spuriously?" If yes → outbox.
// How they compose (this is the part that makes it click)
// They're not three alternatives — they're three layers that often stack:
// Outbox row = durable what to do.
// A loop run under Manager = the always-on consumer that picks it up.
// A Pool inside that consumer = doing a batch of
// 100 rows with 5 in flight.
//
// One sentence each:
// - Pool — parallelism inside one job; caller waits.
// - Manager — lifecycle for loops that never finish.
// - Outbox — durability + atomicity for side effects of a commit.
func NewOutboxWorker(db *gorm.DB, publish PublishFunc, log *slog.Logger, pollInterval time.Duration, batchSize int, opts ...WorkerOption) *OutboxWorker {
	if log == nil {
		log = slog.Default()
	}
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	if batchSize <= 0 {
		batchSize = 10
	}
	w := &OutboxWorker{
		db:           db,
		publish:      publish,
		log:          log,
		pollInterval: pollInterval,
		batchSize:    batchSize,
	}
	for _, o := range opts {
		o(w)
	}
	return w
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
		if len(pending) == 0 && w.exitWhenIdle {
			return nil
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
