package account

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/wssto2/go-core/apperr"
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
	// ActivityByType counts what the person did per record type within the query's actor and
	// days (Within, Outside, Offset and Limit are ignored). A row without a type counts under "".
	ActivityByType(ctx context.Context, q ActivityQuery) (map[string]int, error)
}

// The areas identity itself names.
const (
	// AccountRecord is the record type of an account in the audit trail.
	AccountRecord = "account"
	// IdentityArea holds the changes to accounts unless the application names an area "identity" itself.
	IdentityArea = "identity"
	// OtherArea holds every record type no area names.
	OtherArea = "other"
)

// ActivityArea is the application's name for a kind of record in a person's
// activity: an i18n key such as "crm", for the audit record types it covers.
// Declare one as a value:
//
//	account.Area("crm").Types("customers", "offers").Prefix("contracts.")
type ActivityArea struct {
	key string
	set RecordSet
}

// Area starts an area with the key the application's translations know it by.
func Area(key string) ActivityArea { return ActivityArea{key: key} }

// Key is the area's key.
func (a ActivityArea) Key() string { return a.key }

// Types adds the record types the area covers, named exactly.
func (a ActivityArea) Types(types ...string) ActivityArea {
	a.set.Types = append(slices.Clone(a.set.Types), types...)

	return a
}

// Prefix adds the record types the area covers by how they start, such as "contracts.".
func (a ActivityArea) Prefix(prefixes ...string) ActivityArea {
	a.set.Prefixes = append(slices.Clone(a.set.Prefixes), prefixes...)

	return a
}

// areaKey is what an area key may look like: lower case words, digits, "_" and ".".
var areaKey = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,31}$`)

// ActivityAreas are the areas of an application's records, in order. The first
// area that covers a record type is its area; a type none covers is OtherArea.
// The zero value has only IdentityArea.
type ActivityAreas struct {
	list []ActivityArea
}

// NewActivityAreas checks the areas and adds IdentityArea (the changes to accounts)
// after them unless one is named so. A key must be lower case words, digits, "_" and
// ".", at most 32 characters, unique and not OtherArea; an area must cover at least one
// record type.
func NewActivityAreas(areas ...ActivityArea) (ActivityAreas, error) {
	seen := map[string]bool{}

	for _, a := range areas {
		switch {
		case !areaKey.MatchString(a.key):
			return ActivityAreas{}, fmt.Errorf("identity: activity area %q is not a key: use lower case words, digits, \"_\" and \".\", at most 32 characters, such as \"crm\"", a.key)
		case a.key == OtherArea || a.key == AllArea:
			return ActivityAreas{}, fmt.Errorf("identity: activity area %q is reserved (\"other\" is the record types no area names, \"all\" the count of everything): choose another key", a.key)
		case seen[a.key]:
			return ActivityAreas{}, fmt.Errorf("identity: activity area %q is named twice: name each area once, with all its types", a.key)
		case a.set.Empty() || slices.Contains(a.set.Types, "") || slices.Contains(a.set.Prefixes, ""):
			return ActivityAreas{}, fmt.Errorf("identity: activity area %q covers no record type: give it .Types(...) or .Prefix(...), none of them empty", a.key)
		}

		seen[a.key] = true
	}

	out := ActivityAreas{list: slices.Clone(areas)}
	if !seen[IdentityArea] {
		out.list = append(out.list, Area(IdentityArea).Types(AccountRecord))
	}

	return out, nil
}

func (a ActivityAreas) all() []ActivityArea {
	if a.list == nil {
		return []ActivityArea{Area(IdentityArea).Types(AccountRecord)}
	}

	return a.list
}

// Keys are the area keys, OtherArea last.
func (a ActivityAreas) Keys() []string {
	var keys []string
	for _, area := range a.all() {
		keys = append(keys, area.key)
	}

	return append(keys, OtherArea)
}

// AreaOf is the area of a record type: the first that covers it, else OtherArea.
func (a ActivityAreas) AreaOf(recordType string) string {
	for _, area := range a.all() {
		if area.set.Has(recordType) {
			return area.key
		}
	}

	return OtherArea
}

// sets is what to read for an area: the record types within it, and those outside
// it (every other area's for OtherArea, the areas named before it for the rest, so
// an area shows exactly the rows AreaOf gives it).
func (a ActivityAreas) sets(key string) (within, outside RecordSet, ok bool) {
	for _, area := range a.all() {
		if area.key == key {
			return area.set, outside, true
		}

		outside.Types = append(outside.Types, area.set.Types...)
		outside.Prefixes = append(outside.Prefixes, area.set.Prefixes...)
	}

	return RecordSet{}, outside, key == OtherArea
}

// ActivityFilter narrows a person's activity: to an area (empty: every one) and to days,
// From and To inclusive (zero: open), read as UTC days, the time of day ignored.
type ActivityFilter struct {
	Area string
	From time.Time
	To   time.Time
}

// Activity pages what the person did (IDENTITY-ADMIN-002), newest first, each entry with its area, with
// how many entries match the filter.
func (a *Admin) Activity(ctx context.Context, accountID int, f ActivityFilter, p Paging) ([]ActivityEntry, int, error) {
	if _, err := a.d.Users.Get(ctx, accountID); err != nil {
		return nil, 0, err
	}

	q, err := f.query(accountID)
	if err != nil {
		return nil, 0, err
	}

	if f.Area != "" {
		within, outside, ok := a.d.Areas.sets(f.Area)
		if !ok {
			return nil, 0, invalid("area", ReasonActivityAreaUnknown, map[string]any{"area": f.Area, "areas": a.d.Areas.Keys()})
		}

		q.Within, q.Outside = within, outside
	}

	paging := p.resolved()
	q.Offset, q.Limit = p.offset(), paging.PerPage

	rows, total, err := a.d.Activity.Activity(ctx, q)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}

	for i := range rows {
		rows[i].Area = a.d.Areas.AreaOf(rows[i].RecordType)
	}

	return rows, total, nil
}

// query is the actor and the days of the filter, refusing a range that ends before it starts.
func (f ActivityFilter) query(accountID int) (ActivityQuery, error) {
	q := ActivityQuery{ActorID: accountID}

	if !f.From.IsZero() {
		q.From = day(f.From)
	}

	if !f.To.IsZero() {
		q.To = day(f.To).AddDate(0, 0, 1)
	}

	if !q.From.IsZero() && !q.To.IsZero() && !q.From.Before(q.To) {
		return q, invalid("to", ReasonActivityRangeInvalid)
	}

	return q, nil
}

// AllArea is the key of the count of everything a person did in the days.
const AllArea = "all"

// AreaCount is how many entries an area has.
type AreaCount struct {
	Area  string
	Count int
}

// ActivityCounts counts what the person did in the filter's days, for AllArea first, then each
// area in order, IdentityArea and OtherArea included: the filter's Area is ignored, so the counts
// stay the same whichever area is shown.
func (a *Admin) ActivityCounts(ctx context.Context, accountID int, f ActivityFilter) ([]AreaCount, error) {
	if _, err := a.d.Users.Get(ctx, accountID); err != nil {
		return nil, err
	}

	q, err := f.query(accountID)
	if err != nil {
		return nil, err
	}

	byType, err := a.d.Activity.ActivityByType(ctx, q)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	counts, total := map[string]int{}, 0

	for recordType, n := range byType {
		counts[a.d.Areas.AreaOf(recordType)] += n
		total += n
	}

	out := []AreaCount{{Area: AllArea, Count: total}}
	for _, key := range a.d.Areas.Keys() {
		out = append(out, AreaCount{Area: key, Count: counts[key]})
	}

	return out, nil
}

func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()

	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
