//nolint:exhaustruct,exhaustruct_v5,goconst,gosec,lll,paralleltest,varnamelen // tests in the style of the package's others
package incident_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/incident/gormstore"
)

// The incident on the wire is the first application's, field for field, except that a person is {id, name, email}.
// The incident is written straight into the store with fixed times, and read back through the routes.
func TestTheWireIsTheFirstApplicationsExceptThePeople(t *testing.T) {
	w := newWorld(t)
	store := gormstore.New(w.db)

	first := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	reporter, assignee, duplicate, resolved := ana, petra, 9, time.Date(2026, 1, 3, 8, 0, 0, 0, time.UTC)

	fp := "fp"
	in := core.Incident{
		PublicCode: "INC-1", Fingerprint: fp, Source: core.SourceRuntimeError, Status: core.StatusResolved, Severity: core.SeverityHigh,
		Visibility: core.VisibilityShared, Title: "Something broke", Description: "Detailed description", AffectedModule: "vehicles",
		AffectedRoute: "/api/v1/vehicles", Environment: "production", ErrorClass: "NullPointerException", ErrorCode: "ERR1",
		ErrorMessageSample: "message", StacktraceSample: "stacktrace", FirstSeenAt: first, LastSeenAt: first.Add(24 * time.Hour),
		OccurrenceCount: 3, ReportCount: 2, CommentCount: 1, ReporterUserID: &reporter, AssignedToUserID: &assignee,
		ExternalTicketProvider: core.TicketProviderGitHub, ExternalTicketID: "GH-1", ExternalTicketURL: "https://github.com/org/repo/issues/1",
		DuplicateOfIncidentID: &duplicate, ResolutionSummary: "Fixed", ResolvedAt: &resolved, CreatedAt: first.Add(72 * time.Hour),
		UpdatedAt: first.Add(96 * time.Hour),
	}
	require.NoError(t, store.CreateIncident(t.Context(), &in))

	res := w.as(staff, http.MethodGet, incidents+"/1", nil)
	require.Equal(t, http.StatusOK, res.Code, string(res.Body))

	person := func(id int, name, email string) string {
		return `{"id":` + string(rune('0'+id)) + `,"name":"` + name + `","email":"` + email + `"}`
	}

	assert.JSONEq(t, `{"id":1,"public_code":"INC-1","source":"runtime_error","status":"resolved","severity":"high","visibility":"reporter_and_staff",`+
		`"title":"Something broke","description":"Detailed description","affected_module":"vehicles","affected_route":"/api/v1/vehicles",`+
		`"environment":"production","error_class":"NullPointerException","error_code":"ERR1","error_message_sample":"message",`+
		`"stacktrace_sample":"stacktrace","first_seen_at":"2026-01-01T08:00:00Z","last_seen_at":"2026-01-02T08:00:00Z","occurrence_count":3,`+
		`"report_count":2,"comment_count":1,"reporter_user_id":2,"assigned_to_user_id":4,"external_ticket_provider":"github",`+
		`"external_ticket_id":"GH-1","external_ticket_url":"https://github.com/org/repo/issues/1","duplicate_of_incident_id":9,`+
		`"resolution_summary":"Fixed","resolved_at":"2026-01-03T08:00:00Z","created_at":"2026-01-04T08:00:00Z","updated_at":"2026-01-05T08:00:00Z",`+
		`"reporter":`+person(ana, "Ana Anic", "ana@example.com")+`,"assignee":`+person(petra, "Petra P", "petra@example.com")+`}`, string(res.Data))

	assert.JSONEq(t, `{"comments":[],"reports":[],"watching":false}`, string(res.Meta))
	assert.Contains(t, string(res.Meta), `"comments":[],"reports":[],"watching":false`, "the keys are in the order the first application wrote them")

	// A bare incident omits what it does not have.
	bare := core.Incident{
		PublicCode: "INC-2", Fingerprint: "fp2", Source: core.SourceUserReport, Status: core.StatusNew, Severity: core.SeverityLow,
		Visibility: core.VisibilityShared, Title: "Title", ExternalTicketProvider: core.TicketProviderNone, FirstSeenAt: first, LastSeenAt: first,
		CreatedAt: first, UpdatedAt: first,
	}
	require.NoError(t, store.CreateIncident(t.Context(), &bare))

	res = w.as(staff, http.MethodGet, incidents+"/2", nil)
	assert.JSONEq(t, `{"id":2,"public_code":"INC-2","source":"user_report","status":"new","severity":"low","visibility":"reporter_and_staff",`+
		`"title":"Title","first_seen_at":"2026-01-01T08:00:00Z","last_seen_at":"2026-01-01T08:00:00Z","occurrence_count":0,"report_count":0,`+
		`"comment_count":0,"external_ticket_provider":"none","created_at":"2026-01-01T08:00:00Z","updated_at":"2026-01-01T08:00:00Z"}`, string(res.Data))
}
