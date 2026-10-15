// Package storetest is the conformance suite every incident store must pass.
//
//	func TestMyStore(t *testing.T) {
//		storetest.Run(t, func(t *testing.T) core.Store { return myStore(t) })
//	}
//
// Each subtest gets a fresh, empty store from the factory. The store must be safe for concurrent use.
package storetest

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/pagination"
)

// Factory returns a new empty store for one subtest.
type Factory func(t *testing.T) core.Store

// The people of the suite: the staff, three people and a stranger.
const (
	staffID    = 1
	reporterID = 2
	authorID   = 3
	watcherID  = 4
	strangerID = 5
)

func base() time.Time { return time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC) }

var (
	staff    = core.Viewer{UserID: staffID, Staff: true}
	reporter = core.Viewer{UserID: reporterID}
	author   = core.Viewer{UserID: authorID}
	watcher  = core.Viewer{UserID: watcherID}
	stranger = core.Viewer{UserID: strangerID}
	nobody   = core.Viewer{}
)

// Run runs the whole suite.
func Run(t *testing.T, factory Factory) {
	t.Helper()

	for name, test := range map[string]func(*testing.T, core.Store){
		"an incident round-trips":                       incidentRoundTrips,
		"an update writes every column":                 updateWritesEverything,
		"the open fingerprint is unique":                openFingerprintUnique,
		"a closed incident frees its fingerprint":       closedFreesFingerprint,
		"the public code is unique":                     publicCodeUnique,
		"the latest incident of a fingerprint is found": latestIsFound,
		"visibility follows reporter, author, watcher":  visibilityRules,
		"a staff-only incident is for the staff":        staffOnlyIncident,
		"reports and comments honour the viewer":        reportsAndCommentsVisibility,
		"counting adds to the incident":                 countingAdds,
		"a sighting updates the samples":                sightingSamples,
		"the list pages, filters, searches and orders":  listFeatures,
		"the list honours the visibility":               listVisibility,
		"watching twice is one watcher":                 watchOnce,
		"pruning keeps the newest of each incident":     pruneKeepsNewest,
		"an alert cooldown is stamped and read":         cooldownStamp,
		"a digest item is queued once":                  digestQueuedOnce,
		"a digest is claimed once":                      digestClaimedOnce,
		"claimed digest items are pruned":               digestPruned,
	} {
		t.Run(name, func(t *testing.T) {
			t.Helper()

			test(t, factory(t))
		})
	}
}

// incidentOf is a runtime incident that is open and visible to the staff only.
func incidentOf(code string, mod func(*core.Incident)) core.Incident {
	fp := "fp-" + code
	in := core.Incident{
		PublicCode: code, Fingerprint: fp, OpenFingerprint: &fp, Source: core.SourceRuntimeError, Status: core.StatusNew,
		Severity: core.SeverityHigh, Visibility: core.VisibilityInternal, Title: "Title " + code, Description: "described",
		AffectedModule: "vehicles", AffectedRoute: "/v1/vehicles/:id", Environment: "test", ErrorClass: "*errors.errorString",
		ErrorCode: "internal", ErrorMessageSample: "boom " + code, StacktraceSample: "a.go:1", FirstSeenAt: base(),
		LastSeenAt: base(), OccurrenceCount: 1, ExternalTicketProvider: core.TicketProviderNone, CreatedAt: base(),
		UpdatedAt: base(),
	}

	if mod != nil {
		mod(&in)
	}

	return in
}

func create(t *testing.T, s core.Store, code string, mod func(*core.Incident)) core.Incident {
	t.Helper()

	in := incidentOf(code, mod)
	if err := s.CreateIncident(t.Context(), &in); err != nil {
		t.Fatalf("create %s: %v", code, err)
	}

	if in.ID == 0 {
		t.Fatalf("create %s: no id was set", code)
	}

	return in
}

func must(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

func wantIDs(t *testing.T, what string, got []core.Incident, want ...int) {
	t.Helper()

	ids := make([]int, len(got))
	for i, in := range got {
		ids[i] = in.ID
	}

	if len(ids) != len(want) {
		t.Fatalf("%s: got incidents %v, want %v", what, ids, want)
	}

	for i := range ids {
		if ids[i] != want[i] {
			t.Fatalf("%s: got incidents %v, want %v", what, ids, want)
		}
	}
}

func list(t *testing.T, s core.Store, page pagination.Page, who core.Viewer) *pagination.PageResult[core.Incident] {
	t.Helper()

	res, err := s.List(t.Context(), page, who)
	must(t, err)

	return res
}

func incidentRoundTrips(t *testing.T, s core.Store) {
	reporterRef, assignee := reporterID, authorID
	resolved := base().Add(time.Hour)
	created := create(t, s, "INC-A", func(in *core.Incident) {
		in.ReporterUserID, in.AssignedToUserID, in.ResolvedAt = &reporterRef, &assignee, &resolved
		in.ExternalTicketProvider, in.ExternalTicketID, in.ExternalTicketURL = core.TicketProviderGitHub, "GH-1", "https://example.com/1"
		in.ResolutionSummary = "fixed"
	})

	got, err := s.Find(t.Context(), created.ID, staff)
	must(t, err)

	if got.PublicCode != "INC-A" || got.Title != "Title INC-A" || got.Source != core.SourceRuntimeError ||
		got.Status != core.StatusNew || got.Severity != core.SeverityHigh || got.Visibility != core.VisibilityInternal {
		t.Fatalf("the basics did not round-trip: %+v", got)
	}

	if got.Description != "described" || got.AffectedModule != "vehicles" || got.AffectedRoute != "/v1/vehicles/:id" ||
		got.Environment != "test" || got.ErrorClass != "*errors.errorString" || got.ErrorCode != "internal" ||
		got.ErrorMessageSample != "boom INC-A" || got.StacktraceSample != "a.go:1" {
		t.Fatalf("the texts did not round-trip: %+v", got)
	}

	if !got.FirstSeenAt.Equal(base()) || !got.LastSeenAt.Equal(base()) || got.OccurrenceCount != 1 ||
		got.ResolvedAt == nil || !got.ResolvedAt.Equal(resolved) {
		t.Fatalf("the times and counts did not round-trip: %+v", got)
	}

	if got.ReporterUserID == nil || *got.ReporterUserID != reporterID || got.AssignedToUserID == nil || *got.AssignedToUserID != authorID {
		t.Fatalf("the people did not round-trip: %+v", got)
	}

	if got.ExternalTicketProvider != core.TicketProviderGitHub || got.ExternalTicketID != "GH-1" || got.ResolutionSummary != "fixed" {
		t.Fatalf("the ticket did not round-trip: %+v", got)
	}

	if got.OpenFingerprint == nil || *got.OpenFingerprint != "fp-INC-A" || got.Fingerprint != "fp-INC-A" {
		t.Fatalf("the fingerprints did not round-trip: %+v", got)
	}

	if _, err := s.Find(t.Context(), created.ID+100, staff); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("a missing incident: got %v, want ErrNotFound", err)
	}
}

func updateWritesEverything(t *testing.T, s core.Store) {
	in := create(t, s, "INC-A", func(in *core.Incident) { in.ResolutionSummary, in.ExternalTicketID = "fixed", "GH-1" })

	in.Status, in.OpenFingerprint, in.ResolutionSummary, in.ExternalTicketID = core.StatusIgnored, nil, "", ""
	in.Title = "Renamed"
	in.UpdatedAt = base().Add(time.Hour)

	must(t, s.UpdateIncident(t.Context(), in))

	got, err := s.Find(t.Context(), in.ID, staff)
	must(t, err)

	if got.Title != "Renamed" || got.Status != core.StatusIgnored || got.OpenFingerprint != nil ||
		got.ResolutionSummary != "" || got.ExternalTicketID != "" || !got.UpdatedAt.Equal(base().Add(time.Hour)) {
		t.Fatalf("the update did not write every column, zero values included: %+v", got)
	}

	if !got.CreatedAt.Equal(base()) {
		t.Fatalf("the update changed the creation time: %v", got.CreatedAt)
	}
}

func openFingerprintUnique(t *testing.T, s core.Store) {
	first := create(t, s, "INC-A", nil)

	again := incidentOf("INC-B", func(in *core.Incident) { in.Fingerprint, in.OpenFingerprint = first.Fingerprint, first.OpenFingerprint })
	if err := s.CreateIncident(t.Context(), &again); !errors.Is(err, core.ErrDuplicate) {
		t.Fatalf("a second open incident with one fingerprint: got %v, want ErrDuplicate", err)
	}

	open, err := s.FindOpen(t.Context(), first.Fingerprint)
	must(t, err)

	if open.ID != first.ID {
		t.Fatalf("FindOpen: got incident %d, want %d", open.ID, first.ID)
	}

	if _, err := s.FindOpen(t.Context(), "nothing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("FindOpen of an unknown fingerprint: got %v, want ErrNotFound", err)
	}
}

func closedFreesFingerprint(t *testing.T, s core.Store) {
	first := create(t, s, "INC-A", nil)
	first.Status, first.OpenFingerprint = core.StatusResolved, nil
	must(t, s.UpdateIncident(t.Context(), first))

	if _, err := s.FindOpen(t.Context(), first.Fingerprint); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("FindOpen of a resolved incident: got %v, want ErrNotFound", err)
	}

	again := incidentOf("INC-B", func(in *core.Incident) { in.Fingerprint, in.OpenFingerprint = first.Fingerprint, &first.Fingerprint })
	must(t, s.CreateIncident(t.Context(), &again))
}

func publicCodeUnique(t *testing.T, s core.Store) {
	create(t, s, "INC-A", nil)

	again := incidentOf("INC-A", func(in *core.Incident) { fp := "other"; in.Fingerprint, in.OpenFingerprint = fp, &fp })
	if err := s.CreateIncident(t.Context(), &again); !errors.Is(err, core.ErrDuplicate) {
		t.Fatalf("a second incident with one public code: got %v, want ErrDuplicate", err)
	}
}

func latestIsFound(t *testing.T, s core.Store) {
	older := create(t, s, "INC-A", func(in *core.Incident) {
		in.Fingerprint, in.OpenFingerprint, in.Status = "same", nil, core.StatusResolved
		in.LastSeenAt = base()
	})
	newer := create(t, s, "INC-B", func(in *core.Incident) {
		in.Fingerprint, in.OpenFingerprint, in.Status = "same", nil, core.StatusIgnored
		in.LastSeenAt = base().Add(time.Hour)
	})

	got, err := s.FindLatest(t.Context(), "same")
	must(t, err)

	if got.ID != newer.ID || older.ID == newer.ID {
		t.Fatalf("FindLatest: got incident %d, want the more recently seen %d", got.ID, newer.ID)
	}

	if _, err := s.FindLatest(t.Context(), "unknown"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("FindLatest of an unknown fingerprint: got %v, want ErrNotFound", err)
	}
}

// shared makes an incident that is visible to the people who have a part in it.
func shared(t *testing.T, s core.Store) core.Incident {
	t.Helper()

	reporterRef := reporterID
	in := create(t, s, "INC-S", func(in *core.Incident) {
		in.Visibility, in.Source, in.ReporterUserID = core.VisibilityShared, core.SourceUserReport, &reporterRef
	})

	incidentID := in.ID
	must(t, s.CreateReport(t.Context(), &core.Report{
		IncidentID: &incidentID, ReportedByUserID: authorID, ReportType: "manual", Status: "linked", Title: "T", Description: "D",
		Severity: "medium", CreatedAt: base(), UpdatedAt: base(),
	}))
	must(t, s.Watch(t.Context(), in.ID, watcherID, base()))

	return in
}

func visibilityRules(t *testing.T, s core.Store) {
	in := shared(t, s)

	for name, who := range map[string]core.Viewer{
		"the staff": staff, "the reporter": reporter, "a report author": author, "a watcher": watcher,
	} {
		if _, err := s.Find(t.Context(), in.ID, who); err != nil {
			t.Errorf("%s sees a shared incident: got %v", name, err)
		}
	}

	for name, who := range map[string]core.Viewer{"a stranger": stranger, "nobody": nobody} {
		if _, err := s.Find(t.Context(), in.ID, who); !errors.Is(err, core.ErrNotFound) {
			t.Errorf("%s does not see a shared incident: got %v, want ErrNotFound", name, err)
		}
	}
}

func staffOnlyIncident(t *testing.T, s core.Store) {
	reporterRef := reporterID
	in := create(t, s, "INC-I", func(in *core.Incident) { in.ReporterUserID = &reporterRef })
	must(t, s.Watch(t.Context(), in.ID, watcherID, base()))

	if _, err := s.Find(t.Context(), in.ID, staff); err != nil {
		t.Fatalf("the staff sees a staff-only incident: %v", err)
	}

	for name, who := range map[string]core.Viewer{"its reporter": reporter, "its watcher": watcher} {
		if _, err := s.Find(t.Context(), in.ID, who); !errors.Is(err, core.ErrNotFound) {
			t.Errorf("%s does not see a staff-only incident: got %v, want ErrNotFound", name, err)
		}
	}
}

func reportsAndCommentsVisibility(t *testing.T, s core.Store) {
	in := shared(t, s)
	incidentID := in.ID

	must(t, s.CreateReport(t.Context(), &core.Report{
		IncidentID: &incidentID, ReportedByUserID: reporterID, ReportType: "manual", Status: "linked", Title: "T2", Description: "D2",
		Severity: "high", AttachmentJSON: []byte("[]"), ExtraJSON: []byte(`{"a":1}`), CreatedAt: base().Add(time.Minute),
		UpdatedAt: base().Add(time.Minute),
	}))

	for _, c := range []core.Comment{
		{IncidentID: incidentID, AuthorUserID: authorID, Visibility: core.CommentPublic, Body: "public", CreatedAt: base(), UpdatedAt: base()},
		{IncidentID: incidentID, AuthorUserID: staffID, Visibility: core.CommentInternal, Body: "internal", CreatedAt: base().Add(time.Minute), UpdatedAt: base().Add(time.Minute)},
	} {
		must(t, s.CreateComment(t.Context(), &c))
	}

	deleted := base()
	gone := core.Comment{IncidentID: incidentID, AuthorUserID: staffID, Visibility: core.CommentPublic, Body: "gone", DeletedAt: &deleted, CreatedAt: base(), UpdatedAt: base()}
	must(t, s.CreateComment(t.Context(), &gone))

	reports, err := s.Reports(t.Context(), incidentID, staff)
	must(t, err)

	if len(reports) != 2 || reports[0].ReportedByUserID != authorID || reports[1].ReportedByUserID != reporterID {
		t.Fatalf("the staff sees every report, oldest first: %+v", reports)
	}

	if !sameJSON(reports[1].ExtraJSON, `{"a":1}`) || !sameJSON(reports[1].AttachmentJSON, "[]") {
		t.Fatalf("the JSON documents of a report did not round-trip: %s, %s", reports[1].AttachmentJSON, reports[1].ExtraJSON)
	}

	own, err := s.Reports(t.Context(), incidentID, reporter)
	must(t, err)

	if len(own) != 1 || own[0].ReportedByUserID != reporterID {
		t.Fatalf("a person sees their own reports only: %+v", own)
	}

	all, err := s.Comments(t.Context(), incidentID, staff)
	must(t, err)

	if len(all) != 2 || all[0].Body != "public" || all[1].Body != "internal" {
		t.Fatalf("the staff sees the comments that are not deleted: %+v", all)
	}

	public, err := s.Comments(t.Context(), incidentID, author)
	must(t, err)

	if len(public) != 1 || public[0].Body != "public" {
		t.Fatalf("a person sees the public comments only: %+v", public)
	}
}

// sameJSON reports whether the document is the one wanted, whatever the database did to its white space (MySQL's JSON
// type normalises it).
func sameJSON(got []byte, want string) bool {
	var a, b any

	return json.Unmarshal(got, &a) == nil && json.Unmarshal([]byte(want), &b) == nil && reflect.DeepEqual(a, b)
}

func countingAdds(t *testing.T, s core.Store) {
	in := create(t, s, "INC-A", func(in *core.Incident) { in.Visibility = core.VisibilityShared })
	later := base().Add(time.Hour)

	must(t, s.CountComment(t.Context(), in.ID, later))
	must(t, s.CountComment(t.Context(), in.ID, later))

	reporterRef := reporterID
	must(t, s.CountReport(t.Context(), in.ID, core.SourceMixed, core.VisibilityShared, &reporterRef, later))

	other := authorID
	must(t, s.CountReport(t.Context(), in.ID, core.SourceMixed, core.VisibilityShared, &other, later))

	got, err := s.Find(t.Context(), in.ID, staff)
	must(t, err)

	if got.CommentCount != 2 || got.ReportCount != 2 || got.Source != core.SourceMixed || !got.LastSeenAt.Equal(later) {
		t.Fatalf("counts: %+v", got)
	}

	if got.ReporterUserID == nil || *got.ReporterUserID != reporterID {
		t.Fatalf("the first reporter stays: %v", got.ReporterUserID)
	}
}

func sightingSamples(t *testing.T, s core.Store) {
	in := create(t, s, "INC-A", func(in *core.Incident) { in.Environment, in.AffectedRoute = "test", "/old" })
	later := base().Add(time.Hour)

	sample := core.Occurrence{
		IncidentID: in.ID, Fingerprint: in.Fingerprint, Environment: "production", AffectedRoute: "/new", ErrorClass: "*other",
		ErrorCode: "x", Message: "newer message", Stacktrace: "b.go:2", OccurredAt: later, CreatedAt: later,
	}
	must(t, s.CreateOccurrence(t.Context(), &sample))
	must(t, s.CountOccurrence(t.Context(), in.ID, core.SourceRuntimeError, later, &sample))

	got, err := s.Find(t.Context(), in.ID, staff)
	must(t, err)

	if got.OccurrenceCount != 2 || got.ErrorMessageSample != "newer message" || got.StacktraceSample != "b.go:2" ||
		got.ErrorClass != "*other" || got.AffectedRoute != "/new" || got.Environment != "production" || got.AffectedModule != "vehicles" {
		t.Fatalf("the samples follow the latest sighting, a module it leaves out stays: %+v", got)
	}
}

func listFeatures(t *testing.T, s core.Store) {
	a := create(t, s, "INC-A", func(in *core.Incident) { in.LastSeenAt, in.Title = base().Add(3*time.Hour), "Alpha crash" })
	b := create(t, s, "INC-B", func(in *core.Incident) {
		in.LastSeenAt, in.Title, in.Severity, in.Source = base().Add(2*time.Hour), "Beta crash", core.SeverityLow, core.SourceClientError
	})
	c := create(t, s, "INC-C", func(in *core.Incident) {
		in.LastSeenAt, in.Title, in.Status, in.OpenFingerprint = base().Add(time.Hour), "Gamma slow", core.StatusResolved, nil
	})

	wantIDs(t, "the default order is last seen first", list(t, s, pagination.Page{}, staff).Items, a.ID, b.ID, c.ID)

	res := list(t, s, pagination.Page{Number: 2, Size: 2}, staff)
	wantIDs(t, "the second page of two", res.Items, c.ID)

	if res.Total != 3 || res.LastPage != 2 {
		t.Fatalf("paging numbers: total %d, last page %d", res.Total, res.LastPage)
	}

	filtered := func(key, value string) []core.Incident {
		return list(t, s, pagination.Page{Filters: map[string]string{key: value}}, staff).Items
	}

	wantIDs(t, "status filter", filtered("status", "resolved"), c.ID)
	wantIDs(t, "severity filter", filtered("severity", "low"), b.ID)
	wantIDs(t, "source filter", filtered("source", "client_error"), b.ID)
	wantIDs(t, "search by title", list(t, s, pagination.Page{Search: "crash"}, staff).Items, a.ID, b.ID)
	wantIDs(t, "search by public code", list(t, s, pagination.Page{Search: "INC-C"}, staff).Items, c.ID)
	wantIDs(t, "order by title ascending", list(t, s, pagination.Page{OrderCol: "title", OrderDir: "asc"}, staff).Items, a.ID, b.ID, c.ID)
	wantIDs(t, "order by an unknown column falls back to the id", list(t, s, pagination.Page{OrderCol: "nope", OrderDir: "asc"}, staff).Items, a.ID, b.ID, c.ID)

	if got := list(t, s, pagination.Page{}, staff).Items[0]; got.Description != "" || got.StacktraceSample != "" {
		t.Fatalf("a list row carries no description or stack: %+v", got)
	}
}

func listVisibility(t *testing.T, s core.Store) {
	mine := shared(t, s)
	create(t, s, "INC-I", func(in *core.Incident) { in.LastSeenAt = base().Add(time.Hour) })

	wantIDs(t, "the staff sees both", list(t, s, pagination.Page{}, staff).Items, mine.ID+1, mine.ID)
	wantIDs(t, "the reporter sees the shared one", list(t, s, pagination.Page{}, reporter).Items, mine.ID)
	wantIDs(t, "a report author sees it", list(t, s, pagination.Page{}, author).Items, mine.ID)
	wantIDs(t, "a watcher sees it", list(t, s, pagination.Page{}, watcher).Items, mine.ID)
	wantIDs(t, "a stranger sees none", list(t, s, pagination.Page{}, stranger).Items)
	wantIDs(t, "nobody sees none", list(t, s, pagination.Page{}, nobody).Items)

	if res := list(t, s, pagination.Page{}, stranger); res.Total != 0 {
		t.Fatalf("the total of a stranger is counted over what they see: %d", res.Total)
	}
}

func watchOnce(t *testing.T, s core.Store) {
	in := create(t, s, "INC-A", nil)

	for range 2 {
		must(t, s.Watch(t.Context(), in.ID, watcherID, base()))
	}

	must(t, s.Watch(t.Context(), in.ID, 0, base()))

	if on, err := s.Watching(t.Context(), in.ID, watcherID); err != nil || !on {
		t.Fatalf("watching: got %v, %v", on, err)
	}

	if on, err := s.Watching(t.Context(), in.ID, strangerID); err != nil || on {
		t.Fatalf("a stranger does not watch: got %v, %v", on, err)
	}
}

func pruneKeepsNewest(t *testing.T, s core.Store) {
	a := create(t, s, "INC-A", nil)
	b := create(t, s, "INC-B", nil)

	add := func(in core.Incident, at time.Time) {
		must(t, s.CreateOccurrence(t.Context(), &core.Occurrence{
			IncidentID: in.ID, Fingerprint: in.Fingerprint, Environment: "test", Message: "m", OccurredAt: at, CreatedAt: at,
		}))
	}

	old := base().Add(-100 * 24 * time.Hour)
	add(a, old)
	add(a, old.Add(time.Hour)) // a's newest, old too: kept
	add(b, old)
	add(b, base()) // recent: kept
	add(b, base().Add(time.Hour))

	cutoff := base().Add(-90 * 24 * time.Hour)

	n, err := s.PruneOccurrences(t.Context(), cutoff, 100)
	must(t, err)

	if n != 2 {
		t.Fatalf("pruned %d occurrences, want 2 (a's older one and b's old one)", n)
	}

	n, err = s.PruneOccurrences(t.Context(), cutoff, 100)
	must(t, err)

	if n != 0 {
		t.Fatalf("a second prune deleted %d occurrences, want 0: the newest of an incident stays", n)
	}
}

func cooldownStamp(t *testing.T, s core.Store) {
	if _, ok, err := s.LastAlert(t.Context(), "critical"); err != nil || ok {
		t.Fatalf("no alert yet: got %v, %v", ok, err)
	}

	must(t, s.StampAlert(t.Context(), "critical", base()))
	must(t, s.StampAlert(t.Context(), "critical", base().Add(time.Hour)))
	must(t, s.StampAlert(t.Context(), "high", base()))

	at, ok, err := s.LastAlert(t.Context(), "critical")
	must(t, err)

	if !ok || !at.Equal(base().Add(time.Hour)) {
		t.Fatalf("the stamp is the latest: got %v, %v", at, ok)
	}

	if at, ok, err = s.LastAlert(t.Context(), "high"); err != nil || !ok || !at.Equal(base()) {
		t.Fatalf("a tier has its own stamp: got %v, %v, %v", at, ok, err)
	}
}

func item(id int) core.DigestItem {
	return core.DigestItem{
		IncidentID: id, PublicCode: "INC-" + string(rune('A'+id)), Title: "Title", Severity: core.SeverityMedium,
		CreatedAt: base().Add(time.Duration(id) * time.Minute),
	}
}

func digestQueuedOnce(t *testing.T, s core.Store) {
	must(t, s.QueueDigest(t.Context(), item(1)))
	must(t, s.QueueDigest(t.Context(), item(1)))
	must(t, s.QueueDigest(t.Context(), item(2)))

	items, err := s.ClaimDigest(t.Context(), base().Add(time.Hour), 10)
	must(t, err)

	if len(items) != 2 || items[0].IncidentID != 1 || items[1].IncidentID != 2 || items[0].Severity != core.SeverityMedium ||
		items[0].PublicCode != "INC-B" || !items[1].CreatedAt.Equal(item(2).CreatedAt) {
		t.Fatalf("an incident waits once, oldest first: %+v", items)
	}
}

func digestClaimedOnce(t *testing.T, s core.Store) {
	for id := 1; id <= 3; id++ {
		must(t, s.QueueDigest(t.Context(), item(id)))
	}

	first, err := s.ClaimDigest(t.Context(), base().Add(time.Hour), 2)
	must(t, err)

	if len(first) != 2 {
		t.Fatalf("the limit: claimed %d, want 2", len(first))
	}

	second, err := s.ClaimDigest(t.Context(), base().Add(time.Hour), 10)
	must(t, err)

	if len(second) != 1 || second[0].IncidentID != 3 {
		t.Fatalf("a claimed item is not claimed again: %+v", second)
	}

	if third, err := s.ClaimDigest(t.Context(), base().Add(time.Hour), 10); err != nil || len(third) != 0 {
		t.Fatalf("nothing is left: %+v, %v", third, err)
	}

	// Once claimed, an incident may wait again: it is a new alert.
	must(t, s.QueueDigest(t.Context(), item(1)))

	again, err := s.ClaimDigest(t.Context(), base().Add(2*time.Hour), 10)
	must(t, err)

	if len(again) != 1 || again[0].IncidentID != 1 {
		t.Fatalf("a claimed incident can wait for another digest: %+v", again)
	}
}

func digestPruned(t *testing.T, s core.Store) {
	must(t, s.QueueDigest(t.Context(), item(1)))
	must(t, s.QueueDigest(t.Context(), item(2)))

	_, err := s.ClaimDigest(t.Context(), base(), 1) // takes item 1 at base
	must(t, err)

	n, err := s.PruneDigest(t.Context(), base().Add(time.Hour), 10)
	must(t, err)

	if n != 1 {
		t.Fatalf("pruned %d, want the one claimed before the cutoff", n)
	}

	left, err := s.ClaimDigest(t.Context(), base().Add(2*time.Hour), 10)
	must(t, err)

	if len(left) != 1 || left[0].IncidentID != 2 {
		t.Fatalf("a waiting item is never pruned: %+v", left)
	}
}
