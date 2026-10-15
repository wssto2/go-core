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
	"strconv"
	"testing"
	"time"

	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/pagination"
)

// Factory returns a new empty store for one subtest.
type Factory func(t *testing.T) core.Store

// The people of the suite: the staff, three people with a part in an incident and a stranger.
const (
	staffID    = 1
	reporterID = 2
	authorID   = 3
	watcherID  = 4
	strangerID = 5
)

// The repeated words of the suite.
const (
	testEnv  = "test"
	ticketID = "GH-1"
	summary  = "fixed"
	public   = "public"
	internal = "internal"
)

// The sizes the suite uses.
const (
	retentionDays = 90
	oldDays       = 100
	wide          = 100
	pageOfTwo     = 2
	third         = 3
	hoursInDay    = 24
)

func base() time.Time { return time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC) }

func staff() core.Viewer    { return core.Viewer{UserID: staffID, Staff: true} }
func reporter() core.Viewer { return core.Viewer{UserID: reporterID, Staff: false} }
func author() core.Viewer   { return core.Viewer{UserID: authorID, Staff: false} }
func watcher() core.Viewer  { return core.Viewer{UserID: watcherID, Staff: false} }
func stranger() core.Viewer { return core.Viewer{UserID: strangerID, Staff: false} }
func nobody() core.Viewer   { return core.Viewer{UserID: 0, Staff: false} }

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
		"reports honour the viewer":                     reportsVisibility,
		"comments honour the viewer":                    commentsVisibility,
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

// check fails the test with the error, if there is one.
func check(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

// equal fails the test when the two values differ.
func equal[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()

	if got != want {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

// same fails the test when the two values are not deeply equal.
func same(t *testing.T, what string, got, want any) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

// sameTime fails the test when the two times are not the same instant.
func sameTime(t *testing.T, what string, got, want time.Time) {
	t.Helper()

	if !got.Equal(want) {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

// refused fails the test unless err is target.
func refused(t *testing.T, what string, err, target error) {
	t.Helper()

	if !errors.Is(err, target) {
		t.Fatalf("%s: got %v, want %v", what, err, target)
	}
}

// incidentOf is a runtime incident that is open and visible to the staff only.
func incidentOf(code string, mod func(*core.Incident)) core.Incident {
	key := "fp-" + code

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	incident := core.Incident{
		PublicCode: code, Fingerprint: key, OpenFingerprint: &key, Source: core.SourceRuntimeError, Status: core.StatusNew,
		Severity: core.SeverityHigh, Visibility: core.VisibilityInternal, Title: "Title " + code, Description: "described",
		AffectedModule: "vehicles", AffectedRoute: "/v1/vehicles/:id", Environment: testEnv,
		ErrorClass: "*errors.errorString", ErrorCode: internal, ErrorMessageSample: "boom " + code,
		StacktraceSample: "a.go:1", FirstSeenAt: base(), LastSeenAt: base(), OccurrenceCount: 1,
		ExternalTicketProvider: core.TicketProviderNone, CreatedAt: base(), UpdatedAt: base(),
	}

	if mod != nil {
		mod(&incident)
	}

	return incident
}

func create(t *testing.T, store core.Store, code string, mod func(*core.Incident)) core.Incident {
	t.Helper()

	incident := incidentOf(code, mod)
	check(t, store.CreateIncident(t.Context(), &incident))

	if incident.ID == 0 {
		t.Fatalf("create %s: no id was set", code)
	}

	return incident
}

// ids are the ids of the incidents.
func ids(incidents []core.Incident) []int {
	out := make([]int, len(incidents))
	for i, incident := range incidents {
		out[i] = incident.ID
	}

	return out
}

func list(t *testing.T, store core.Store, page pagination.Page, who core.Viewer) *pagination.PageResult[core.Incident] {
	t.Helper()

	result, err := store.List(t.Context(), page, who)
	check(t, err)

	return result
}

func find(t *testing.T, store core.Store, incidentID int) core.Incident {
	t.Helper()

	incident, err := store.Find(t.Context(), incidentID, staff())
	check(t, err)

	return incident
}

func incidentRoundTrips(t *testing.T, store core.Store) {
	t.Helper()

	reportedBy, assignee, resolved := reporterID, authorID, base().Add(time.Hour)
	created := create(t, store, "INC-A", func(incident *core.Incident) {
		incident.ReporterUserID, incident.AssignedToUserID, incident.ResolvedAt = &reportedBy, &assignee, &resolved
		incident.ExternalTicketProvider, incident.ExternalTicketID = core.TicketProviderGitHub, ticketID
		incident.ExternalTicketURL, incident.ResolutionSummary = "https://example.com/1", summary
	})

	got := find(t, store, created.ID)

	equal(t, "public code", got.PublicCode, "INC-A")
	equal(t, "title", got.Title, "Title INC-A")
	equal(t, "source", got.Source, core.SourceRuntimeError)
	equal(t, "status", got.Status, core.StatusNew)
	equal(t, "severity", got.Severity, core.SeverityHigh)
	equal(t, "visibility", got.Visibility, core.VisibilityInternal)
	equal(t, "description", got.Description, "described")
	equal(t, "module", got.AffectedModule, "vehicles")
	equal(t, "route", got.AffectedRoute, "/v1/vehicles/:id")
	equal(t, "environment", got.Environment, testEnv)
	equal(t, "class", got.ErrorClass, "*errors.errorString")
	equal(t, "code", got.ErrorCode, internal)
	equal(t, "message sample", got.ErrorMessageSample, "boom INC-A")
	equal(t, "stack sample", got.StacktraceSample, "a.go:1")
	equal(t, "occurrences", got.OccurrenceCount, 1)
	equal(t, "provider", got.ExternalTicketProvider, core.TicketProviderGitHub)
	equal(t, "ticket", got.ExternalTicketID, ticketID)
	equal(t, "resolution", got.ResolutionSummary, summary)
	sameTime(t, "first seen", got.FirstSeenAt, base())
	sameTime(t, "last seen", got.LastSeenAt, base())
	same(t, "reporter", got.ReporterUserID, &reportedBy)
	same(t, "assignee", got.AssignedToUserID, &assignee)
	same(t, "fingerprint", got.OpenFingerprint, created.OpenFingerprint)
	equal(t, "closing time is set", got.ResolvedAt != nil, true)
	sameTime(t, "resolved", *got.ResolvedAt, resolved)

	_, err := store.Find(t.Context(), created.ID+wide, staff())
	refused(t, "a missing incident", err, core.ErrNotFound)
}

func updateWritesEverything(t *testing.T, store core.Store) {
	t.Helper()

	incident := create(t, store, "INC-A", func(incident *core.Incident) {
		incident.ResolutionSummary, incident.ExternalTicketID = summary, ticketID
	})

	incident.Status, incident.OpenFingerprint, incident.ResolutionSummary, incident.ExternalTicketID =
		core.StatusIgnored, nil, "", ""
	incident.Title = "Renamed"
	incident.UpdatedAt = base().Add(time.Hour)

	check(t, store.UpdateIncident(t.Context(), incident))

	got := find(t, store, incident.ID)

	equal(t, "title", got.Title, "Renamed")
	equal(t, "status", got.Status, core.StatusIgnored)
	equal(t, "open fingerprint is cleared", got.OpenFingerprint == nil, true)
	equal(t, "resolution is cleared", got.ResolutionSummary, "")
	equal(t, "ticket is cleared", got.ExternalTicketID, "")
	sameTime(t, "updated", got.UpdatedAt, base().Add(time.Hour))
	sameTime(t, "created", got.CreatedAt, base())
}

func openFingerprintUnique(t *testing.T, store core.Store) {
	t.Helper()

	first := create(t, store, "INC-A", nil)

	again := incidentOf("INC-B", func(incident *core.Incident) {
		incident.Fingerprint, incident.OpenFingerprint = first.Fingerprint, first.OpenFingerprint
	})
	refused(t, "a second open incident with one fingerprint", store.CreateIncident(t.Context(), &again), core.ErrDuplicate)

	open, err := store.FindOpen(t.Context(), first.Fingerprint)
	check(t, err)
	equal(t, "the open incident", open.ID, first.ID)

	_, err = store.FindOpen(t.Context(), "nothing")
	refused(t, "an unknown fingerprint", err, core.ErrNotFound)
}

func closedFreesFingerprint(t *testing.T, store core.Store) {
	t.Helper()

	first := create(t, store, "INC-A", nil)
	first.Status, first.OpenFingerprint = core.StatusResolved, nil
	check(t, store.UpdateIncident(t.Context(), first))

	_, err := store.FindOpen(t.Context(), first.Fingerprint)
	refused(t, "a resolved incident is not open", err, core.ErrNotFound)

	again := incidentOf("INC-B", func(incident *core.Incident) {
		incident.Fingerprint, incident.OpenFingerprint = first.Fingerprint, &first.Fingerprint
	})
	check(t, store.CreateIncident(t.Context(), &again))
}

func publicCodeUnique(t *testing.T, store core.Store) {
	t.Helper()

	create(t, store, "INC-A", nil)

	other := "other"
	again := incidentOf("INC-A", func(incident *core.Incident) {
		incident.Fingerprint, incident.OpenFingerprint = other, &other
	})
	refused(t, "a second incident with one public code", store.CreateIncident(t.Context(), &again), core.ErrDuplicate)
}

func latestIsFound(t *testing.T, store core.Store) {
	t.Helper()

	closed := func(incident *core.Incident) { incident.Fingerprint, incident.OpenFingerprint = "same", nil }

	create(t, store, "INC-A", func(incident *core.Incident) {
		closed(incident)
		incident.Status = core.StatusResolved
	})
	newer := create(t, store, "INC-B", func(incident *core.Incident) {
		closed(incident)
		incident.Status, incident.LastSeenAt = core.StatusIgnored, base().Add(time.Hour)
	})

	got, err := store.FindLatest(t.Context(), "same")
	check(t, err)
	equal(t, "the more recently seen", got.ID, newer.ID)

	_, err = store.FindLatest(t.Context(), "unknown")
	refused(t, "an unknown fingerprint", err, core.ErrNotFound)
}

// shared makes an incident that is visible to the people who have a part in it.
func shared(t *testing.T, store core.Store) core.Incident {
	t.Helper()

	reportedBy := reporterID
	incident := create(t, store, "INC-S", func(incident *core.Incident) {
		incident.Visibility, incident.Source = core.VisibilityShared, core.SourceUserReport
		incident.ReporterUserID = &reportedBy
	})

	incidentID := incident.ID

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	check(t, store.CreateReport(t.Context(), &core.Report{
		ID: 0, IncidentID: &incidentID, ReportedByUserID: authorID, ReportType: "manual", Status: "linked", Title: "T",
		Description: "D", Severity: "medium", CreatedAt: base(), UpdatedAt: base(),
	}))
	check(t, store.Watch(t.Context(), incident.ID, watcherID, base()))

	return incident
}

func visibilityRules(t *testing.T, store core.Store) {
	t.Helper()

	incident := shared(t, store)

	for name, who := range map[string]core.Viewer{
		"the staff": staff(), "the reporter": reporter(), "a report author": author(), "a watcher": watcher(),
	} {
		_, err := store.Find(t.Context(), incident.ID, who)
		if err != nil {
			t.Errorf("%s sees a shared incident: got %v", name, err)
		}
	}

	for name, who := range map[string]core.Viewer{"a stranger": stranger(), "nobody": nobody()} {
		_, err := store.Find(t.Context(), incident.ID, who)
		refused(t, name+" does not see a shared incident", err, core.ErrNotFound)
	}
}

func staffOnlyIncident(t *testing.T, store core.Store) {
	t.Helper()

	reportedBy := reporterID
	incident := create(t, store, "INC-I", func(incident *core.Incident) { incident.ReporterUserID = &reportedBy })
	check(t, store.Watch(t.Context(), incident.ID, watcherID, base()))

	_, err := store.Find(t.Context(), incident.ID, staff())
	check(t, err)

	for name, who := range map[string]core.Viewer{"its reporter": reporter(), "its watcher": watcher()} {
		_, err = store.Find(t.Context(), incident.ID, who)
		refused(t, name+" does not see a staff-only incident", err, core.ErrNotFound)
	}
}

func reportsVisibility(t *testing.T, store core.Store) {
	t.Helper()

	incident := shared(t, store)
	incidentID := incident.ID
	later := base().Add(time.Minute)

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	check(t, store.CreateReport(t.Context(), &core.Report{
		ID: 0, IncidentID: &incidentID, ReportedByUserID: reporterID, ReportType: "manual", Status: "linked", Title: "T2",
		Description: "D2", Severity: "high", AttachmentJSON: []byte("[]"), ExtraJSON: []byte(`{"a":1}`), CreatedAt: later,
		UpdatedAt: later,
	}))

	reports, err := store.Reports(t.Context(), incidentID, staff())
	check(t, err)
	equal(t, "the staff sees every report", len(reports), pageOfTwo)
	equal(t, "oldest first", reports[0].ReportedByUserID, authorID)
	equal(t, "newest last", reports[1].ReportedByUserID, reporterID)
	equal(t, "extra document", sameJSON(reports[1].ExtraJSON, `{"a":1}`), true)
	equal(t, "attachment document", sameJSON(reports[1].AttachmentJSON, "[]"), true)

	own, err := store.Reports(t.Context(), incidentID, reporter())
	check(t, err)
	equal(t, "a person sees their own reports only", len(own), 1)
	equal(t, "and it is theirs", own[0].ReportedByUserID, reporterID)
}

func commentsVisibility(t *testing.T, store core.Store) {
	t.Helper()

	incident := shared(t, store)
	later, deleted := base().Add(time.Minute), base()

	remark := func(author int, visibility core.CommentVisibility, body string, at time.Time) core.Comment {
		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		return core.Comment{
			IncidentID: incident.ID, AuthorUserID: author, Visibility: visibility, Body: body, CreatedAt: at, UpdatedAt: at,
		}
	}

	gone := remark(staffID, public, "gone", base())
	gone.DeletedAt = &deleted

	for _, comment := range []core.Comment{
		remark(authorID, public, public, base()), remark(staffID, internal, internal, later), gone,
	} {
		check(t, store.CreateComment(t.Context(), &comment))
	}

	all, err := store.Comments(t.Context(), incident.ID, staff())
	check(t, err)
	equal(t, "the staff sees the comments that are not deleted", len(all), pageOfTwo)
	equal(t, "oldest first", all[0].Body, public)
	equal(t, "internal last", all[1].Body, internal)

	seen, err := store.Comments(t.Context(), incident.ID, author())
	check(t, err)
	equal(t, "a person sees the public comments only", len(seen), 1)
	equal(t, "and it is public", seen[0].Body, public)
}

// sameJSON reports whether the document is the one wanted, whatever the database did to its white space (MySQL's JSON
// type normalises it).
func sameJSON(got []byte, want string) bool {
	var have, wanted any

	return json.Unmarshal(got, &have) == nil && json.Unmarshal([]byte(want), &wanted) == nil &&
		reflect.DeepEqual(have, wanted)
}

func countingAdds(t *testing.T, store core.Store) {
	t.Helper()

	incident := create(t, store, "INC-A", func(incident *core.Incident) { incident.Visibility = core.VisibilityShared })
	later := base().Add(time.Hour)
	first, second := reporterID, authorID

	check(t, store.CountComment(t.Context(), incident.ID, later))
	check(t, store.CountComment(t.Context(), incident.ID, later))
	check(t, store.CountReport(t.Context(), incident.ID, core.SourceMixed, core.VisibilityShared, &first, later))
	check(t, store.CountReport(t.Context(), incident.ID, core.SourceMixed, core.VisibilityShared, &second, later))

	got := find(t, store, incident.ID)

	equal(t, "comments", got.CommentCount, pageOfTwo)
	equal(t, "reports", got.ReportCount, pageOfTwo)
	equal(t, "source", got.Source, core.SourceMixed)
	sameTime(t, "last seen", got.LastSeenAt, later)
	same(t, "the first reporter stays", got.ReporterUserID, &first)
}

func sightingSamples(t *testing.T, store core.Store) {
	t.Helper()

	incident := create(t, store, "INC-A", func(incident *core.Incident) {
		incident.Environment, incident.AffectedRoute = testEnv, "/old"
	})
	later := base().Add(time.Hour)

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	sample := core.Occurrence{
		IncidentID: incident.ID, Fingerprint: incident.Fingerprint, Environment: "production", AffectedRoute: "/new",
		ErrorClass: "*other", ErrorCode: "x", Message: "newer message", Stacktrace: "b.go:2", OccurredAt: later,
		CreatedAt: later,
	}
	check(t, store.CreateOccurrence(t.Context(), &sample))
	check(t, store.CountOccurrence(t.Context(), incident.ID, core.SourceRuntimeError, later, &sample))

	got := find(t, store, incident.ID)

	equal(t, "occurrences", got.OccurrenceCount, pageOfTwo)
	equal(t, "message sample", got.ErrorMessageSample, "newer message")
	equal(t, "stack sample", got.StacktraceSample, "b.go:2")
	equal(t, "class", got.ErrorClass, "*other")
	equal(t, "route", got.AffectedRoute, "/new")
	equal(t, "environment", got.Environment, "production")
	equal(t, "a module the sighting leaves out stays", got.AffectedModule, "vehicles")
}

func listFeatures(t *testing.T, store core.Store) {
	t.Helper()

	alpha := create(t, store, "INC-A", func(incident *core.Incident) {
		incident.LastSeenAt, incident.Title = base().Add(third*time.Hour), "Alpha crash"
	})
	beta := create(t, store, "INC-B", func(incident *core.Incident) {
		incident.LastSeenAt, incident.Title = base().Add(pageOfTwo*time.Hour), "Beta crash"
		incident.Severity, incident.Source = core.SeverityLow, core.SourceClientError
	})
	gamma := create(t, store, "INC-C", func(incident *core.Incident) {
		incident.LastSeenAt, incident.Title = base().Add(time.Hour), "Gamma slow"
		incident.Status, incident.OpenFingerprint = core.StatusResolved, nil
	})

	all := []int{alpha.ID, beta.ID, gamma.ID}
	byPage := func(page pagination.Page) []int { return ids(list(t, store, page, staff()).Items) }
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	filtered := func(key, value string) []int { return byPage(pagination.Page{Filters: map[string]string{key: value}}) }

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	same(t, "the default order is last seen first", byPage(pagination.Page{}), all)
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	same(t, "the second page of two", byPage(pagination.Page{Number: pageOfTwo, Size: pageOfTwo}), []int{gamma.ID})
	same(t, "status filter", filtered("status", "resolved"), []int{gamma.ID})
	same(t, "severity filter", filtered("severity", "low"), []int{beta.ID})
	same(t, "source filter", filtered("source", "client_error"), []int{beta.ID})
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	same(t, "search by title", byPage(pagination.Page{Search: "crash"}), []int{alpha.ID, beta.ID})
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	same(t, "search by public code", byPage(pagination.Page{Search: "INC-C"}), []int{gamma.ID})
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	same(t, "order by title", byPage(pagination.Page{OrderCol: "title", OrderDir: "asc"}), all)
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	same(t, "an unknown column falls back to the id", byPage(pagination.Page{OrderCol: "nope", OrderDir: "asc"}), all)

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	paged := list(t, store, pagination.Page{Number: pageOfTwo, Size: pageOfTwo}, staff())

	equal(t, "total", int(paged.Total), len(all))
	equal(t, "last page", paged.LastPage, pageOfTwo)

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	row := list(t, store, pagination.Page{}, staff()).Items[0]

	equal(t, "a list row carries no description", row.Description, "")
	equal(t, "a list row carries no stack", row.StacktraceSample, "")
}

func listVisibility(t *testing.T, store core.Store) {
	t.Helper()

	mine := shared(t, store)
	other := create(t, store, "INC-I", func(incident *core.Incident) { incident.LastSeenAt = base().Add(time.Hour) })

	for _, scenario := range []struct {
		name string
		who  core.Viewer
		want []int
	}{
		{"the staff sees both", staff(), []int{other.ID, mine.ID}},
		{"the reporter sees the shared one", reporter(), []int{mine.ID}},
		{"a report author sees it", author(), []int{mine.ID}},
		{"a watcher sees it", watcher(), []int{mine.ID}},
		{"a stranger sees none", stranger(), []int{}},
		{"nobody sees none", nobody(), []int{}},
	} {
		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		result := list(t, store, pagination.Page{}, scenario.who)

		same(t, scenario.name, ids(result.Items), scenario.want)
		equal(t, scenario.name+": the total is counted over what they see", int(result.Total), len(scenario.want))
	}
}

func watchOnce(t *testing.T, store core.Store) {
	t.Helper()

	incident := create(t, store, "INC-A", nil)

	for range pageOfTwo {
		check(t, store.Watch(t.Context(), incident.ID, watcherID, base()))
	}

	check(t, store.Watch(t.Context(), incident.ID, 0, base()))

	watching, err := store.Watching(t.Context(), incident.ID, watcherID)
	check(t, err)
	equal(t, "the watcher watches", watching, true)

	watching, err = store.Watching(t.Context(), incident.ID, strangerID)
	check(t, err)
	equal(t, "a stranger does not", watching, false)
}

func pruneKeepsNewest(t *testing.T, store core.Store) {
	t.Helper()

	alpha, beta := create(t, store, "INC-A", nil), create(t, store, "INC-B", nil)

	add := func(incident core.Incident, at time.Time) {
		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		check(t, store.CreateOccurrence(t.Context(), &core.Occurrence{
			IncidentID: incident.ID, Fingerprint: incident.Fingerprint, Environment: testEnv, Message: "m", OccurredAt: at,
			CreatedAt: at,
		}))
	}

	old := base().Add(-oldDays * hoursInDay * time.Hour)

	add(alpha, old)
	add(alpha, old.Add(time.Hour)) // alpha's newest, old too: kept
	add(beta, old)
	add(beta, base()) // recent: kept
	add(beta, base().Add(time.Hour))

	cutoff := base().Add(-retentionDays * hoursInDay * time.Hour)

	pruned, err := store.PruneOccurrences(t.Context(), cutoff, wide)
	check(t, err)
	equal(t, "alpha's older one and beta's old one", pruned, pageOfTwo)

	pruned, err = store.PruneOccurrences(t.Context(), cutoff, wide)
	check(t, err)
	equal(t, "the newest of an incident stays, whatever its age", pruned, 0)
}

func cooldownStamp(t *testing.T, store core.Store) {
	t.Helper()

	_, found, err := store.LastAlert(t.Context(), "critical")
	check(t, err)
	equal(t, "no alert yet", found, false)

	check(t, store.StampAlert(t.Context(), "critical", base()))
	check(t, store.StampAlert(t.Context(), "critical", base().Add(time.Hour)))
	check(t, store.StampAlert(t.Context(), "high", base()))

	stamped, found, err := store.LastAlert(t.Context(), "critical")
	check(t, err)
	equal(t, "an alert was sent", found, true)
	sameTime(t, "the stamp is the latest", stamped, base().Add(time.Hour))

	stamped, found, err = store.LastAlert(t.Context(), "high")
	check(t, err)
	equal(t, "a tier has its own stamp", found, true)
	sameTime(t, "the other tier", stamped, base())
}

func item(incidentID int) core.DigestItem {
	return core.DigestItem{
		ID: 0, IncidentID: incidentID, PublicCode: "INC-" + strconv.Itoa(incidentID), Title: "Title",
		Severity: core.SeverityMedium, CreatedAt: base().Add(time.Duration(incidentID) * time.Minute),
	}
}

func queue(t *testing.T, store core.Store, incidentIDs ...int) {
	t.Helper()

	for _, incidentID := range incidentIDs {
		check(t, store.QueueDigest(t.Context(), item(incidentID)))
	}
}

func claimed(t *testing.T, store core.Store, at time.Time, limit int) []int {
	t.Helper()

	items, err := store.ClaimDigest(t.Context(), at, limit)
	check(t, err)

	out := make([]int, len(items))
	for i, taken := range items {
		out[i] = taken.IncidentID
	}

	return out
}

func digestQueuedOnce(t *testing.T, store core.Store) {
	t.Helper()

	queue(t, store, 1, 1, pageOfTwo)

	items, err := store.ClaimDigest(t.Context(), base().Add(time.Hour), wide)
	check(t, err)
	equal(t, "an incident waits once", len(items), pageOfTwo)
	equal(t, "oldest first", items[0].IncidentID, 1)
	equal(t, "severity", items[0].Severity, core.SeverityMedium)
	equal(t, "public code", items[0].PublicCode, "INC-1")
	sameTime(t, "queued at", items[1].CreatedAt, item(pageOfTwo).CreatedAt)
}

func digestClaimedOnce(t *testing.T, store core.Store) {
	t.Helper()

	queue(t, store, 1, pageOfTwo, third)

	same(t, "the limit", claimed(t, store, base().Add(time.Hour), pageOfTwo), []int{1, pageOfTwo})
	same(t, "a claimed item is not claimed again", claimed(t, store, base().Add(time.Hour), wide), []int{third})
	same(t, "nothing is left", claimed(t, store, base().Add(time.Hour), wide), []int{})

	// Once claimed, an incident may wait again: it is a new alert.
	queue(t, store, 1)
	again := claimed(t, store, base().Add(pageOfTwo*time.Hour), wide)
	same(t, "a claimed incident can wait for another digest", again, []int{1})
}

func digestPruned(t *testing.T, store core.Store) {
	t.Helper()

	queue(t, store, 1, pageOfTwo)
	same(t, "the first is taken", claimed(t, store, base(), 1), []int{1})

	pruned, err := store.PruneDigest(t.Context(), base().Add(time.Hour), wide)
	check(t, err)
	equal(t, "the one claimed before the cutoff", pruned, 1)

	same(t, "a waiting item is never pruned", claimed(t, store, base().Add(pageOfTwo*time.Hour), wide), []int{pageOfTwo})
}
