package account

import (
	"context"
	"slices"
	"strings"
	"time"
)

// ActivityAction is what a person did to a record, as the activity of a person
// shows it. The audit trail's own verbs differ by writer ("create", "created",
// "delete", "password"); ActivityActionOf sorts them into the three the client
// can rely on.
type ActivityAction string

// The actions of a person's activity.
const (
	ActivityCreated ActivityAction = "created"
	ActivityChanged ActivityAction = "changed"
	ActivityDeleted ActivityAction = "deleted"
)

// ActivityActionOf sorts an audit action into created, changed or deleted: the
// verbs "create"/"created" and "delete"/"deleted" are those, anything else
// ("update", "password", "deactivated") is a change to the record.
func ActivityActionOf(audit string) ActivityAction {
	switch strings.ToLower(strings.TrimSpace(audit)) {
	case "create", "created":
		return ActivityCreated
	case "delete", "deleted":
		return ActivityDeleted
	default:
		return ActivityChanged
	}
}

// ActivityEntry is one thing a person did: a record they created, changed or
// deleted.
type ActivityEntry struct {
	ID int
	// Area is the application's name for the kind of record (see ActivityAreas);
	// the log leaves it empty, Admin.Activity fills it.
	Area       string
	RecordType string
	RecordID   int
	Action     ActivityAction
	// SignedInAs is who was signed in as the person when they did it, zero when
	// nobody was: the entry may be that somebody's.
	SignedInAs int
	At         time.Time
}

// RecordSet selects audit record types: the ones named exactly and the ones
// starting with a prefix. The zero set selects nothing.
type RecordSet struct {
	Types    []string
	Prefixes []string
}

// Empty says whether the set selects nothing.
func (s RecordSet) Empty() bool { return len(s.Types) == 0 && len(s.Prefixes) == 0 }

// Has says whether the set selects the record type.
func (s RecordSet) Has(recordType string) bool {
	return slices.Contains(s.Types, recordType) || slices.ContainsFunc(s.Prefixes, func(p string) bool { return strings.HasPrefix(recordType, p) })
}

// ActivityQuery asks for a page of what a person did, newest first. Within and
// Outside narrow it by record type (Within empty: every type; Outside: those left
// out); From is inclusive and To exclusive, zero is open. Limit is the page size.
type ActivityQuery struct {
	ActorID int
	Within  RecordSet
	Outside RecordSet
	From    time.Time
	To      time.Time
	Offset  int
	Limit   int
}

// ActivityLog reads what people did from the audit trail. Activity returns a
// page and how many entries match; each entry says who was signed in as the
// person when it was done. The default is gormstore.NewActivityLog.
type ActivityLog interface {
	Activity(ctx context.Context, q ActivityQuery) ([]ActivityEntry, int, error)
}
