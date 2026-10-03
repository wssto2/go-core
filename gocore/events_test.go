package gocore

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
)

type ticketAssigned struct {
	TicketID int `json:"ticket_id"`
}

var assigned = event.Define[ticketAssigned]("tickets.assigned")

func ok(context.Context, ticketAssigned) error { return nil }

func TestCheckReportsEventProblemsWithTheirFix(t *testing.T) {
	app := testApp(t, "local")
	app.Events(assigned.To("notices", ok), assigned.To("notices", ok), assigned.To("Bad Name", ok))

	err := app.Check()

	var se *StartupError
	if !errors.As(err, &se) {
		t.Fatalf("want *StartupError, got %v", err)
	}

	for _, want := range []string{`two consumers are named "notices"`, `consumer name "Bad Name"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message misses %q:\n%s", want, err)
		}
	}

	for _, p := range se.Problems {
		if p.Fix == "" {
			t.Errorf("problem without a fix: %+v", p)
		}
	}
}

func TestEventsWithoutADatabaseIsAProblem(t *testing.T) {
	app := New(bootstrap.DefaultConfig(), WithLogger(slog.New(slog.DiscardHandler)))
	app.Events(assigned.To("notices", ok))

	if err := app.Check(); err == nil || !strings.Contains(err.Error(), "primary database") {
		t.Fatalf("want a problem naming the database, got %v", err)
	}
}

func TestEventsRegistersTheQueueTablesOnce(t *testing.T) {
	app := testApp(t, "local")
	app.Events(assigned.To("a", ok))
	app.Events(assigned.To("b", ok))
	app.Events()

	if len(app.migrations) != 1 {
		t.Fatalf("want the event migrations collected once, got %d sources", len(app.migrations))
	}
}

// Run starts a worker per consumer, handles what a transaction published, and
// stops the workers at shutdown.
func TestRunHandlesPublishedEventsAndStopsTheWorkers(t *testing.T) {
	app, _ := runnable(t)
	app.autoMigrate = t.Context()

	got := make(chan int, 1)
	app.Events(assigned.To("notices", func(_ context.Context, a ticketAssigned) error {
		got <- a.TicketID

		return nil
	}))

	err := database.NewTransactor(app.Database()).WithinTransaction(t.Context(), func(ctx context.Context) error {
		return assigned.Publish(ctx, ticketAssigned{TicketID: 7})
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- app.RunContext(ctx) }()

	select {
	case id := <-got:
		if id != 7 {
			t.Fatalf("handled ticket %d", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the event was not handled")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop the consumer workers")
	}
}

// Events adds the housekeeper of the queue's tables once, with or without consumers; an application that
// never calls it has no queue and no housekeeper.
func TestEventsAddsTheHousekeeper(t *testing.T) {
	app := testApp(t, "local")
	if got := len(app.eventWorkers()); got != 0 {
		t.Fatalf("%d event workers before Events", got)
	}

	app.Events()

	names := []string{}
	for _, w := range app.eventWorkers() {
		names = append(names, w.Name())
	}

	if len(names) != 1 || names[0] != "event.housekeeping" {
		t.Fatalf("workers after Events with no consumers: %v", names)
	}

	app.Events(assigned.To("notices", ok))

	if got := len(app.eventWorkers()); got != 2 {
		t.Fatalf("want the consumer's worker and the housekeeper, got %d", got)
	}
}
