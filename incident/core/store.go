package core

import (
	"context"
	"errors"
	"time"

	"github.com/wssto2/go-core/pagination"
)

// The errors of a Store.
var (
	// ErrNotFound is a lookup that found no incident, or none the viewer may see. The two are the same on purpose: a
	// person must not learn that an incident they cannot see exists.
	ErrNotFound = errors.New("incident not found")
	// ErrDuplicate is CreateIncident meeting an incident that is already there: the public code, or the open
	// fingerprint of an incident created a moment before by a concurrent request.
	ErrDuplicate = errors.New("incident already exists")
	// ErrConflict is a write that lost a race with another transaction to the database (a deadlock, a lock wait that
	// ran out). Nothing was written; the Service tries again.
	ErrConflict = errors.New("write conflict")
)

// Store keeps incidents. The gormstore package is the SQL implementation and storetest the suite every store passes.
// A method joins the transaction in ctx when it carries one (database.TxFromContext).
type Store interface {
	Reader
	Writer
	Alerts
	Housekeeping
}

// Reader is what a store answers about incidents.
type Reader interface {
	// List is a page of the incidents the viewer may see, last seen first unless the page orders otherwise. It
	// filters on the page's status, severity and source filters, and searches the public code, title, message sample,
	// module and route.
	List(ctx context.Context, page pagination.Page, viewer Viewer) (*pagination.PageResult[Incident], error)
	// Find returns the incident if the viewer may see it, else ErrNotFound.
	Find(ctx context.Context, incidentID int, viewer Viewer) (Incident, error)
	// Reports returns the reports of an incident, oldest first: all of them for the staff, the viewer's own for
	// anybody else.
	Reports(ctx context.Context, incidentID int, viewer Viewer) ([]Report, error)
	// Comments returns the comments of an incident that are not deleted, oldest first: all of them for the staff,
	// the public ones for anybody else.
	Comments(ctx context.Context, incidentID int, viewer Viewer) ([]Comment, error)
	// Occurrences is a page of the sightings of an incident, newest first, if the viewer may see the incident, else
	// ErrNotFound. The page's number and size apply; its order, search and filters do not.
	Occurrences(
		ctx context.Context, incidentID int, page pagination.Page, viewer Viewer,
	) (*pagination.PageResult[Occurrence], error)
	// Watching reports whether the person watches the incident.
	Watching(ctx context.Context, incidentID, userID int) (bool, error)
	// FindOpen returns the open incident with the fingerprint, else ErrNotFound.
	FindOpen(ctx context.Context, fingerprint string) (Incident, error)
	// FindLatest returns the most recently seen incident with the fingerprint, open or not, else ErrNotFound.
	FindLatest(ctx context.Context, fingerprint string) (Incident, error)
}

// Writer is what a store changes about incidents.
type Writer interface {
	// CreateIncident stores a new incident and sets its ID. ErrDuplicate when the public code or the open
	// fingerprint is taken.
	CreateIncident(ctx context.Context, incident *Incident) error
	// UpdateIncident writes every column of the incident, zero values included, except its id and creation time.
	UpdateIncident(ctx context.Context, incident Incident) error
	CreateReport(ctx context.Context, report *Report) error
	CreateComment(ctx context.Context, comment *Comment) error
	CreateOccurrence(ctx context.Context, occurrence *Occurrence) error
	// CountComment adds one to the incident's comment count.
	CountComment(ctx context.Context, incidentID int, at time.Time) error
	// CountReport adds one report: the count, the last seen time, the source and visibility, and the reporter when
	// the incident has none.
	CountReport(ctx context.Context, incidentID int, source Source, visibility string, reporterID *int, at time.Time) error
	// CountOccurrence adds one sighting: the count, the last seen time, the source, and the samples of the latest.
	CountOccurrence(ctx context.Context, incidentID int, source Source, at time.Time, sample *Occurrence) error
	// Watch makes the person a watcher of the incident from the time; watching twice changes nothing.
	Watch(ctx context.Context, incidentID, userID int, at time.Time) error
}

// Alerts is the store of the alerts: the cooldown of each severity and the digest queue.
type Alerts interface {
	// LastAlert is when an immediate alert of the severity tier was last sent, false if never.
	LastAlert(ctx context.Context, tier string) (time.Time, bool, error)
	// StampAlert records that an immediate alert of the tier was sent at the time.
	StampAlert(ctx context.Context, tier string, at time.Time) error
	// QueueDigest adds a new incident to the next digest, unless it is already waiting in it.
	QueueDigest(ctx context.Context, item DigestItem) error
	// ClaimDigest marks the waiting digest items as taken at the time and returns them, oldest first, at most limit.
	// Call it in a transaction: the rows it takes are locked until it commits.
	ClaimDigest(ctx context.Context, at time.Time, limit int) ([]DigestItem, error)
}

// Housekeeping is what a store deletes.
type Housekeeping interface {
	// PruneOccurrences deletes up to limit occurrences that happened before the time, except the newest one of each
	// incident, and says how many it deleted.
	PruneOccurrences(ctx context.Context, before time.Time, limit int) (int, error)
	// PruneDigest deletes up to limit digest items that were claimed before the time and says how many.
	PruneDigest(ctx context.Context, before time.Time, limit int) (int, error)
}

// DigestItem is a new incident waiting for the next digest alert, because its severity is not an immediate one or its
// immediate alert was held back by the cooldown.
type DigestItem struct {
	ID         int
	IncidentID int
	PublicCode string
	Title      string
	Severity   Severity
	CreatedAt  time.Time
}

// People names people by id.
type People interface {
	// Find returns the people that exist among ids; an id nobody has is simply absent.
	Find(ctx context.Context, ids []int) ([]Person, error)
}

// Transactor runs a function in a transaction that the stores called with its context join. database.Transactor is one.
type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock tells the time.
type Clock interface{ Now() time.Time }
