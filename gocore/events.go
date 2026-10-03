package gocore

import (
	"context"

	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/event/migrations"
	"github.com/wssto2/go-core/worker"
)

// Events collects the consumers of events a feature reacts to, and with them
// the tables the event queue lives in (the outbox and each consumer's attempts)
// on the primary connection. Run starts one worker per consumer, each claiming
// the events of its name, retrying a failing one with a backoff and setting it
// aside as a dead letter after its attempts; shutdown stops them with the other
// background work. A housekeeper runs beside them and deletes the events every
// consumer finished more than 30 days ago, dead letters excepted.
//
//	app.Events(Assigned.To("notifications.assignee", notices.Assigned))
//
// A feature that only publishes calls Events with no consumers, so the outbox
// table exists for its Publish. Delivery is at least once: see event.Event.To.
func (a *App) Events(consumers ...event.Consumer) {
	if a.registry == nil {
		a.fail("a feature uses events but no database is configured",
			"add a connection to the database config, or pass gocore.WithRegistry: the event queue lives in the primary database")

		return
	}

	a.Schema(Schema{Files: migrations.Files, Models: event.Migrate})

	a.eventsUsed = true
	a.consumers = append(a.consumers, consumers...)
}

// queue is the event queue of the collected consumers, nil when there are none.
func (a *App) queue() *event.Queue {
	if len(a.consumers) == 0 {
		return nil
	}

	return event.NewQueue(a.Database(), a.clock, a.log, a.consumers...)
}

// eventWorkers are the background workers of the collected consumers, and the
// housekeeper that deletes events processed more than event.Retention ago (30
// days; dead letters are kept) once any feature uses the queue.
func (a *App) eventWorkers() []worker.Worker {
	var out []worker.Worker

	if q := a.queue(); q != nil {
		out = q.Workers()
	}

	if a.eventsUsed {
		out = append(out, event.NewHousekeeper(a.Database(), a.clock, a.log))
	}

	return out
}

// DrainEvents hands every due event to its consumers now and returns what their
// handlers returned, for tests: gocoretest.Publish calls it. A failing event is
// recorded for retry as it is when running.
func (a *App) DrainEvents(ctx context.Context) error {
	q := a.queue()
	if q == nil {
		return nil
	}

	return q.Drain(ctx)
}

func eventProblems(consumers []event.Consumer) []Problem {
	var out []Problem

	for _, p := range event.Problems(consumers) {
		out = append(out, Problem{What: p.What, Fix: p.Fix})
	}

	return out
}
