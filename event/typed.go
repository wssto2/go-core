package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/observability/tracing"
	"gorm.io/gorm"
)

// Event is a fact one feature publishes and others react to: a name, a version
// and the payload type that travels with it. Declare it once as a value:
//
//	var Assigned = event.Define[TicketAssigned]("tickets.assigned")
//
// The name is stored with every queued row, so it never changes with the Go
// type: moving TicketAssigned to another package does not orphan the rows
// already waiting. Delivery is at least once; see Consumer.
type Event[T any] struct {
	name    string
	version int
}

// Define declares an event. The name is lower-case words joined by dots or
// dashes ("tickets.assigned"); gocore's Check refuses anything else. The
// version is 1 until Version says otherwise.
func Define[T any](name string) Event[T] {
	return Event[T]{name: name, version: 1}
}

// Version returns the event at version n. Raise it only when the payload's
// shape changes in a way old rows cannot be read as: a consumer of version 2
// sets a row written as version 1 aside as a dead letter instead of guessing.
func (e Event[T]) Version(n int) Event[T] {
	e.version = n

	return e
}

// Name is the durable name the event is stored under.
func (e Event[T]) Name() string { return e.name }

// Publish queues the event for every consumer of it. It joins the transaction
// in ctx (database.Transactor.WithinTransaction), so the event is queued if and
// only if the caller's own writes commit:
//
//	err := transactor.WithinTransaction(ctx, func(ctx context.Context) error {
//		if err := tickets.Assign(ctx, id, user); err != nil {
//			return err
//		}
//
//		return Assigned.Publish(ctx, TicketAssigned{TicketID: id, UserID: user})
//	})
//
// Without a transaction in ctx, Publish fails with an error that says so: an
// event published outside the write it reports could be lost or announced for a
// write that rolled back.
func (e Event[T]) Publish(ctx context.Context, payload T) error {
	tx, ok := database.TxFromContext(ctx)
	if !ok {
		return fmt.Errorf("event %q: Publish needs the transaction of the write it reports in ctx: "+
			"call it inside database.Transactor.WithinTransaction", e.name)
	}

	return e.publishOn(ctx, tx, payload)
}

// publishOn writes the event on db, which is a transaction or, in tests, a plain connection.
func (e Event[T]) publishOn(ctx context.Context, db *gorm.DB, payload T) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("event %q: the payload cannot be encoded as JSON: %w", e.name, err)
	}

	traceID, ok := tracing.TraceIDFromContext(ctx)
	if !ok || traceID == "" {
		traceID = uuid.NewString()
	}

	env := Envelope{
		Version:   strconv.Itoa(e.version),
		RequestID: traceID,
		Timestamp: time.Now().UTC(),
		Source:    DefaultEventSource,
		Payload:   raw,
	}

	body, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("event %q: %w", e.name, err)
	}

	row := OutboxEvent{RequestID: env.RequestID, Source: env.Source, EventType: e.name, Envelope: body}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("event %q: queue it: %w", e.name, err)
	}

	return nil
}

// To makes a consumer of the event: handler runs once per published event, with
// the payload decoded.
//
//	app.Events(Assigned.To("notifications.assignee", notices.Assigned))
//
// consumer is the durable name the queue keeps this consumer's attempts under.
// It is part of what is stored, so choose it once and keep it when the Go
// function is renamed or moved; two consumers never share one. Lower-case words
// joined by dots or dashes, at most 100 characters.
//
// Delivery is at least once. A handler that fails is called again after a
// delay (Retry), and one whose lease ran out while it was still working can run
// beside a second delivery of the same event, so a handler must tolerate being
// called twice for one event: key what it writes on ID(ctx).
func (e Event[T]) To(consumer string, handler func(ctx context.Context, payload T) error) Consumer {
	return Consumer{
		name:    consumer,
		event:   e.name,
		version: e.version,
		payload: reflect.TypeFor[T](),
		retry:   defaultRetry,
		handle: func(ctx context.Context, raw json.RawMessage) error {
			var payload T
			if err := json.Unmarshal(raw, &payload); err != nil {
				return fmt.Errorf("%w: payload: %w", ErrMalformed, err)
			}

			return handler(ctx, payload)
		},
	}
}

// ErrMalformed marks an event that can never be handled, whatever the handler
// does: its envelope or payload cannot be read. The queue does not retry it; it
// goes straight to the dead letters. A handler may return it (wrapped) for a
// payload it finds unusable.
var ErrMalformed = errors.New("malformed event")

// Consumer is a handler bound to an event under a durable name, made by
// Event.To and collected with gocore's App.Events.
type Consumer struct {
	name    string
	event   string
	version int
	// payload is the Go type the event carries, kept only so Problems can tell
	// two Define calls with one name apart; the stored name never comes from it.
	payload reflect.Type
	retry   retryPolicy
	handle  func(ctx context.Context, payload json.RawMessage) error
}

// Name is the consumer's durable name.
func (c Consumer) Name() string { return c.name }

// EventName is the name of the event the consumer handles.
func (c Consumer) EventName() string { return c.event }

// Retry sets how a failing handler is retried, in place of the defaults: 5
// attempts, 5 seconds doubling up to 30 minutes.
//
//	Assigned.To("notifications.assignee", notices.Assigned).Retry(event.Attempts(10))
func (c Consumer) Retry(opts ...RetryOption) Consumer {
	for _, o := range opts {
		o(&c.retry)
	}

	return c
}

type retryPolicy struct {
	attempts   int
	firstDelay time.Duration
	maxDelay   time.Duration
}

var defaultRetry = retryPolicy{attempts: 5, firstDelay: 5 * time.Second, maxDelay: 30 * time.Minute}

// RetryOption adjusts Consumer.Retry.
type RetryOption func(*retryPolicy)

// Attempts is how many failures in a row set an event aside as a dead letter.
func Attempts(n int) RetryOption { return func(p *retryPolicy) { p.attempts = n } }

// Backoff is the wait after the first failure and the longest wait: it doubles
// per failure from first up to longest.
func Backoff(first, longest time.Duration) RetryOption {
	return func(p *retryPolicy) { p.firstDelay, p.maxDelay = first, longest }
}

// delay is the wait after the given number of failures.
func (p retryPolicy) delay(failures int) time.Duration {
	d := p.firstDelay
	for i := 1; i < failures && d < p.maxDelay; i++ {
		d *= 2
	}

	return min(d, p.maxDelay)
}

// Problem is something wrong with the consumers: what, and what to do about it.
type Problem struct {
	What string
	Fix  string
}

// problems lists what is wrong with the consumer.
func (c Consumer) problems() []Problem {
	var out []Problem

	if !namePattern.MatchString(c.event) {
		out = append(out, Problem{fmt.Sprintf("event name %q is not lower-case words joined by dots or dashes", c.event),
			"rename it in event.Define, for example \"tickets.assigned\""})
	}

	if !namePattern.MatchString(c.name) || len(c.name) > 100 {
		out = append(out, Problem{fmt.Sprintf("consumer name %q is not lower-case words joined by dots or dashes, at most 100 characters", c.name),
			"fix the first argument of To, for example \"notifications.assignee\""})
	}

	if c.version < 1 {
		out = append(out, Problem{fmt.Sprintf("event %q has version %d", c.event, c.version), "versions start at 1: drop the Version call or pass 2 or more"})
	}

	if c.retry.attempts < 1 || c.retry.firstDelay <= 0 || c.retry.maxDelay < c.retry.firstDelay {
		out = append(out, Problem{fmt.Sprintf("consumer %q has a retry policy that cannot work", c.name),
			"Attempts needs at least 1, and Backoff a positive first delay not above the longest"})
	}

	return out
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9]*([.-][a-z0-9]+)*$`)

// Problems reports what is wrong with a set of consumers: bad names, two
// consumers with one name, and two Define calls that share a name but not a
// payload type or version. gocore's Check calls it.
func Problems(consumers []Consumer) []Problem {
	var out []Problem

	byName := map[string]bool{}
	first := map[string]Consumer{}

	for _, c := range consumers {
		out = append(out, c.problems()...)

		if byName[c.name] {
			out = append(out, Problem{fmt.Sprintf("two consumers are named %q", c.name),
				"the queue keeps attempts per consumer name, so give each its own"})
		}

		byName[c.name] = true

		if f, ok := first[c.event]; ok && (f.version != c.version || f.payload != c.payload) {
			out = append(out, Problem{fmt.Sprintf("event %q is defined twice, as %v v%d and as %v v%d", c.event, f.payload, f.version, c.payload, c.version),
				"declare it once and share the value, or give the second its own name"})
		} else if !ok {
			first[c.event] = c
		}
	}

	return out
}
