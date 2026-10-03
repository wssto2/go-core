package event_test

import (
	"context"
	"fmt"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// An event is a value declared once. Its name is what is stored, so it is
// chosen by hand and survives renaming the Go type.
func ExampleDefine() {
	assigned := event.Define[TicketAssigned]("tickets.assigned")
	changed := assigned.Version(2) // only when the payload's shape changes

	fmt.Println(assigned.Name(), changed.Name())
	// Output: tickets.assigned tickets.assigned
}

// A consumer has a durable name of its own; Retry replaces the defaults.
func ExampleEvent_To() {
	consumer := Assigned.
		To("notifications.assignee", func(_ context.Context, a TicketAssigned) error {
			fmt.Println("notify", a.UserID)

			return nil
		}).
		Retry(event.Attempts(10))

	fmt.Println(consumer.Name(), "handles", consumer.EventName())
	// Output: notifications.assignee handles tickets.assigned
}

// Problems is what gocore's Check reports about the collected consumers.
func ExampleProblems() {
	handler := func(context.Context, TicketAssigned) error { return nil }

	for _, problem := range event.Problems([]event.Consumer{
		Assigned.To("notices", handler),
		Assigned.To("notices", handler),
	}) {
		fmt.Println(problem)
	}
	// Output: two consumers are named "notices": the queue keeps attempts per consumer name, so each needs its own
}

// Publish joins the transaction of the write it reports: the event is queued if
// and only if that transaction commits.
func ExampleEvent_Publish() {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	_ = event.EnsureOutboxSchema(db)

	err := database.NewTransactor(db).WithinTransaction(context.Background(), func(ctx context.Context) error {
		// ... the ticket is assigned here, in the same transaction ...
		return Assigned.Publish(ctx, TicketAssigned{TicketID: 7, UserID: 3})
	})

	var queued int64

	db.Model(&event.OutboxEvent{}).Count(&queued)
	fmt.Println(err, queued)
	// Output: <nil> 1
}
