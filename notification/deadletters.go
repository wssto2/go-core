package notification

import (
	"context"
	"errors"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/route"
)

// The permissions the dead-letter routes are guarded by: events.deadletter:view and
// events.deadletter:retry. The ids are fixed. Dead letters span every consumer of the application (not
// only the notification ones), so the permissions are the event queue's.
const (
	// ViewDeadLetters is seeing the events consumers gave up on.
	ViewDeadLetters = "events.deadletter:view"
	// RetryDeadLetters is putting them back in the queue. It needs ViewDeadLetters.
	RetryDeadLetters = "events.deadletter:retry"
)

// DefinePermissions adds ViewDeadLetters and RetryDeadLetters to the catalogue when it lacks them.
// Call it on the application's catalogue before access.Install builds the engine, so the administrator's
// role holds them: start-up checks that every route's permission is defined.
func DefinePermissions(c *authz.Catalogue) error {
	for _, def := range []struct {
		id   string
		opts []authz.DefineOption
	}{
		{ViewDeadLetters, nil},
		{RetryDeadLetters, []authz.DefineOption{authz.Sensitive(), authz.Requires(ViewDeadLetters)}},
	} {
		if _, ok := c.Lookup(def.id); ok {
			continue
		}

		if err := c.Define(def.id, def.opts...); err != nil {
			return err
		}
	}

	return nil
}

// DeadLettersInput is a page of the dead letters, optionally of one consumer.
type DeadLettersInput struct {
	// Consumer limits the list to one consumer's dead letters; empty lists all.
	Consumer string `query:"consumer" json:"consumer,omitempty" validation:"max:100"`
	Page     int    `query:"page" json:"page,omitempty"`
	PerPage  int    `query:"per_page" json:"per_page,omitempty"`
}

// DeadLetterRow is an event a consumer gave up on, newest first.
type DeadLetterRow struct {
	EventID  uint64 `json:"event_id"`
	Consumer string `json:"consumer"`
	// EventName is empty when the event was pruned from the queue; such a letter cannot be retried.
	EventName      string     `json:"event_name"`
	EventCreatedAt *time.Time `json:"event_created_at"`
	Attempts       int        `json:"attempts"`
	LastError      string     `json:"last_error"`
	DeadAt         time.Time  `json:"dead_at"`
	Retryable      bool       `json:"retryable"`
}

// RetryInput addresses one dead letter: the event and the consumer that gave up on it.
type RetryInput struct {
	EventID  uint64 `path:"event" validation:"required"`
	Consumer string `path:"consumer" validation:"required|max:100"`
}

// RetryAllInput names the consumer whose dead letters are all put back in the queue.
type RetryAllInput struct {
	Consumer string `json:"consumer" validation:"required|max:100"`
}

// Retried says how many dead letters were put back in the queue.
type Retried struct {
	Retried int `json:"retried"`
}

const deadLetters = base + "/events/dead-letters"

// DeadLetterRoutes is the declared contract of the dead-letter routes: generic over every consumer of
// the application's event queue, mounted by Install. Paths under /v1, before the application's prefix.
var DeadLetterRoutes = DeclareDeadLetters().Contract()

// DeadLetterDeclared is the dead-letter routes as values.
type DeadLetterDeclared struct {
	// List is a page of the dead letters, newest first, optionally of one consumer.
	List route.Route[DeadLettersInput, datatable.DatatableResult[DeadLetterRow]]
	// Retry puts one consumer's dead letter back in the queue.
	Retry route.Route[RetryInput, Retried]
	// RetryAll puts every dead letter of one consumer back in the queue.
	RetryAll route.Route[RetryAllInput, Retried]

	group *route.Contract
}

// DeclareDeadLetters declares the dead-letter routes, guarded by the module's fixed permissions.
func DeclareDeadLetters() *DeadLetterDeclared {
	d := &DeadLetterDeclared{
		List: route.Get[DeadLettersInput, datatable.DatatableResult[DeadLetterRow]](deadLetters).
			Name("events.dead-letters.list").Requires(ViewDeadLetters),
		Retry: route.Post[RetryInput, Retried](deadLetters + "/:event/:consumer/retry").
			Name("events.dead-letters.retry").Requires(RetryDeadLetters),
		RetryAll: route.Post[RetryAllInput, Retried](deadLetters + "/retry").
			Name("events.dead-letters.retry-all").Requires(RetryDeadLetters),
	}

	d.group = route.Group("events", d.List, d.Retry, d.RetryAll)

	return d
}

// Contract is the routes as a group: what contract.Generate reads.
func (d *DeadLetterDeclared) Contract() *route.Contract { return d.group }

// To binds a handler to every route; hand the result to app.Routes.
func (d *DeadLetterDeclared) To(letters *event.DeadLetters) []route.Handled {
	h := deadLetterHandlers{letters: letters}

	return []route.Handled{d.List.To(h.list), d.Retry.To(h.retry), d.RetryAll.To(h.retryAll)}
}

type deadLetterHandlers struct{ letters *event.DeadLetters }

func (h deadLetterHandlers) list(ctx context.Context, in DeadLettersInput) (datatable.DatatableResult[DeadLetterRow], error) {
	perPage := in.PerPage
	if perPage < 1 {
		perPage = 25
	}

	perPage = min(perPage, 100)
	page := max(in.Page, 1)

	letters, total, err := h.letters.List(ctx, event.DeadLetterQuery{Consumer: in.Consumer, Limit: perPage, Offset: (page - 1) * perPage})
	if err != nil {
		return datatable.DatatableResult[DeadLetterRow]{}, apperr.Internal(err)
	}

	rows := make([]DeadLetterRow, 0, len(letters))
	for _, l := range letters {
		rows = append(rows, DeadLetterRow{
			EventID: l.EventID, Consumer: l.Consumer, EventName: l.EventName, EventCreatedAt: l.EventCreatedAt,
			Attempts: l.Attempts, LastError: l.LastError, DeadAt: l.DeadAt, Retryable: l.Retryable(),
		})
	}

	out := datatable.DatatableResult[DeadLetterRow]{
		Data: rows, Total: int64(total), PerPage: perPage, Page: page, LastPage: max((total+perPage-1)/perPage, 1),
	}

	if len(rows) > 0 {
		out.From = (page-1)*perPage + 1
		out.To = out.From + len(rows) - 1
	}

	return out, nil
}

// retry puts one dead letter back in the queue. A letter retried meanwhile is not found (404); one whose
// event was pruned cannot be retried (409) and changes nothing.
func (h deadLetterHandlers) retry(ctx context.Context, in RetryInput) (Retried, error) {
	err := h.letters.Retry(ctx, in.EventID, in.Consumer)

	switch {
	case errors.Is(err, event.ErrDeadLetterNotFound):
		return Retried{}, apperr.NotFound("dead letter not found")
	case errors.Is(err, event.ErrDeadLetterEventGone):
		return Retried{}, apperr.ConflictErr(err)
	case err != nil:
		return Retried{}, apperr.Internal(err)
	}

	return Retried{Retried: 1}, nil
}

func (h deadLetterHandlers) retryAll(ctx context.Context, in RetryAllInput) (Retried, error) {
	n, err := h.letters.RetryAll(ctx, in.Consumer)
	if err != nil {
		return Retried{}, apperr.Internal(err)
	}

	return Retried{Retried: n}, nil
}
