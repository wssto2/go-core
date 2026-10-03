package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/wssto2/go-core/worker"
	"gorm.io/gorm"
)

type idKey struct{}

// ID is the queue's id of the event being handled, for a handler that must
// tolerate a second delivery of it: key what the handler writes on it.
func ID(ctx context.Context) (uint64, bool) {
	id, ok := ctx.Value(idKey{}).(uint64)

	return id, ok
}

// Queue runs consumers over the outbox: each consumer claims the events of its
// type on its own, hands them to its handler, and records the outcome: done,
// retried after a backoff, or, after its attempts, set aside as a dead letter.
// gocore's App.Events builds one; Workers are what it runs in the background.
type Queue struct {
	db        *gorm.DB
	log       *slog.Logger
	clock     Clock
	consumers []Consumer
}

// NewQueue returns the queue of consumers over db. A nil clock is the system
// clock, a nil log slog.Default.
func NewQueue(db *gorm.DB, clock Clock, log *slog.Logger, consumers ...Consumer) *Queue {
	if clock == nil {
		clock = systemClock{}
	}

	if log == nil {
		log = slog.Default()
	}

	return &Queue{db: db, log: log, clock: clock, consumers: consumers}
}

// siblings are the names of every consumer of the event, the ones whose
// finishing lets the outbox row be marked processed.
func (q *Queue) siblings(eventName string) []string {
	var names []string

	for _, c := range q.consumers {
		if c.event == eventName {
			names = append(names, c.name)
		}
	}

	return names
}

// Workers returns one background worker per consumer.
func (q *Queue) Workers() []worker.Worker {
	out := make([]worker.Worker, 0, len(q.consumers))
	for _, c := range q.consumers {
		out = append(out, &consumerWorker{queue: q, consumer: c})
	}

	return out
}

// Drain handles every event that is due, for every consumer, once and then
// until none is left, and returns what the handlers returned. It is for tests:
// a failing event is recorded for retry as always, and reported here.
func (q *Queue) Drain(ctx context.Context) error {
	var errs []error

	for _, c := range q.consumers {
		w := &consumerWorker{queue: q, consumer: c}

		for {
			n, handlerErrs, err := w.tick(ctx)
			errs = append(errs, handlerErrs...)

			if err != nil {
				errs = append(errs, err)

				break
			}

			if n < batchSize {
				break
			}
		}
	}

	return errors.Join(errs...)
}

type consumerWorker struct {
	queue    *Queue
	consumer Consumer
}

// Name identifies the worker in logs.
func (w *consumerWorker) Name() string { return "event.consumer." + w.consumer.name }

// Run polls until ctx is cancelled; a full batch is followed by another at once.
func (w *consumerWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		n, _, err := w.tick(ctx)
		if err != nil && ctx.Err() == nil {
			w.queue.log.ErrorContext(ctx, "event: claiming failed", "consumer", w.consumer.name, "error", err)
		}

		if n >= batchSize && err == nil && ctx.Err() == nil {
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// tick claims one batch and handles it. It returns how many events were
// claimed, the errors the handlers returned, and a claim failure.
func (w *consumerWorker) tick(ctx context.Context) (int, []error, error) {
	q, c := w.queue, w.consumer
	now := whole(q.clock.Now())

	due, err := claim(ctx, q.db, c.name, c.event, now, now.Add(claimLease), batchSize)
	if err != nil {
		return 0, nil, err
	}

	var errs []error

	for i, ev := range due {
		if ctx.Err() != nil {
			return i, errs, nil // the leases end and the rest come due again
		}

		if err := w.process(ctx, ev); err != nil {
			errs = append(errs, fmt.Errorf("consumer %q, event %d: %w", c.name, ev.ID, err))
		}
	}

	return len(due), errs, nil
}

// process handles one claimed event and records the outcome. It returns the
// handler's error.
func (w *consumerWorker) process(ctx context.Context, ev claimedEvent) error {
	q, c := w.queue, w.consumer

	handleErr := w.handle(ctx, ev)
	if handleErr != nil && ctx.Err() != nil {
		return handleErr // stopped mid-event (shutdown): not its failure; the lease ends and it comes due again
	}

	now := whole(q.clock.Now())
	row := attemptRow{EventID: ev.ID, Consumer: c.name, Attempts: ev.Attempts, NextAttemptAt: now, UpdatedAt: now}
	malformed := errors.Is(handleErr, ErrMalformed)

	switch {
	case handleErr == nil:
		row.DoneAt = &now
	case malformed || ev.Attempts+1 >= c.retry.attempts:
		row.Attempts++
		row.LastError = truncateError(handleErr.Error())
		row.DeadAt = &now
	default:
		row.Attempts++
		row.LastError = truncateError(handleErr.Error())
		row.NextAttemptAt = now.Add(c.retry.delay(row.Attempts))
	}

	if err := finish(ctx, q.db, row, q.siblings(c.event)); err != nil {
		q.log.ErrorContext(ctx, "event: recording the outcome failed", "consumer", c.name, "event_id", ev.ID, "error", err, "handler_error", handleErr)

		return handleErr
	}

	switch {
	case handleErr == nil:
	case row.DeadAt != nil:
		q.log.ErrorContext(ctx, "event: giving up, kept as a dead letter in "+attemptsTable,
			"consumer", c.name, "event_id", ev.ID, "event", c.event, "attempts", row.Attempts, "error", handleErr)
	default:
		q.log.WarnContext(ctx, "event: handler failed, will retry",
			"consumer", c.name, "event_id", ev.ID, "event", c.event, "attempt", row.Attempts, "next_attempt_at", row.NextAttemptAt, "error", handleErr)
	}

	return handleErr
}

// handle decodes the envelope, checks the version and calls the handler. A
// panic in the handler is a failure of that event, not of the worker.
func (w *consumerWorker) handle(ctx context.Context, ev claimedEvent) (err error) {
	c := w.consumer

	var env Envelope
	if err := json.Unmarshal([]byte(ev.Envelope), &env); err != nil {
		return fmt.Errorf("%w: envelope: %w", ErrMalformed, err)
	}

	// Rows written by InsertOutboxEvent carry version "1".
	if env.Version != strconv.Itoa(c.version) {
		return fmt.Errorf("%w: written as version %q but consumer %q handles version %d: "+
			"keep a consumer for each version still queued, or retry it once the payload is readable", ErrMalformed, env.Version, c.name, c.version)
	}

	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("handler panicked: %v", p)
		}
	}()

	return c.handle(context.WithValue(ctx, idKey{}, ev.ID), env.Payload)
}
