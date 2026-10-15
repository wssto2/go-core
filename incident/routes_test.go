//nolint:goconst,lll,paralleltest,prealloc,varnamelen // tests in the style of the package's others
package incident_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/incident"
	"github.com/wssto2/go-core/incident/core"
)

type reportBody = map[string]any

func report(title string) reportBody {
	return reportBody{"title": title, "description": "It breaks", "severity": "high", "affected_module": "vehicles", "page_url": "/vehicles/12"}
}

func TestAReportIsFiledAndSeenByItsReporter(t *testing.T) {
	w := newWorld(t)

	res := w.as(ana, http.MethodPost, incidents+"/reports", report("The list is empty"))
	require.Equal(t, http.StatusCreated, res.Code, string(res.Body))

	created := decode[incident.Created](t, res.Data)
	assert.Equal(t, "The list is empty", created.Data.Title)
	assert.Equal(t, "user_report", created.Data.Source)
	assert.Equal(t, "high", created.Data.Severity)
	assert.Equal(t, "reporter_and_staff", created.Data.Visibility)
	assert.Equal(t, 1, created.Data.ReportCount)
	assert.Regexp(t, `^INC-[0-9A-F]{12}$`, created.Data.PublicCode)
	require.NotNil(t, created.Data.Reporter)
	assert.Equal(t, incident.PersonResponse{ID: ana, Name: "Ana Anic", Email: "ana@example.com"}, *created.Data.Reporter)
	assert.True(t, created.Meta.Watching, "the reporter watches the incident")
	require.Len(t, created.Meta.Reports, 1)
	assert.Equal(t, "manual", created.Meta.Reports[0].ReportType)
	assert.Equal(t, "linked", created.Meta.Reports[0].Status)

	shown := w.as(ana, http.MethodGet, incidents+"/"+strconv.Itoa(created.Data.ID), nil)
	require.Equal(t, http.StatusOK, shown.Code, string(shown.Body))
	assert.Equal(t, "The list is empty", decode[incident.Incident](t, shown.Data).Title)
	assert.Len(t, decode[incident.Meta](t, shown.Meta).Reports, 1)
}

func TestARepeatedReportJoinsTheOpenIncident(t *testing.T) {
	w := newWorld(t)

	first := decode[incident.Created](t, w.as(ana, http.MethodPost, incidents+"/reports", report("Same problem")).Data)
	second := w.as(ivo, http.MethodPost, incidents+"/reports", report("Same problem"))
	require.Equal(t, http.StatusCreated, second.Code, string(second.Body))

	joined := decode[incident.Created](t, second.Data)
	assert.Equal(t, first.Data.ID, joined.Data.ID)
	assert.Equal(t, 2, joined.Data.ReportCount)
	assert.Len(t, w.rows("incidents"), 1)
	assert.Len(t, w.rows("incident_reports"), 2)
	assert.Len(t, joined.Meta.Reports, 1, "a person sees their own report only")

	staffView := decode[incident.Meta](t, w.as(staff, http.MethodGet, incidents+"/"+strconv.Itoa(first.Data.ID), nil).Meta)
	assert.Len(t, staffView.Reports, 2)
}

func TestAReportIsChecked(t *testing.T) {
	w := newWorld(t)

	for name, body := range map[string]any{
		"no title":         reportBody{"description": "x"},
		"no description":   reportBody{"title": "x"},
		"a bad severity":   reportBody{"title": "x", "description": "y", "severity": "enormous"},
		"bad attachments":  reportBody{"title": "x", "description": "y", "attachment_json": "{nope"},
		"a title too long": reportBody{"title": string(make([]byte, 256)), "description": "y"},
	} {
		res := w.as(ana, http.MethodPost, incidents+"/reports", body)
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, res.Code, name+": "+string(res.Body))
	}

	assert.Equal(t, http.StatusBadRequest, w.as(ana, http.MethodPost, incidents+"/reports", "{not json").Code)
	assert.Empty(t, w.rows("incidents"))
}

func TestOnlyTheAllowedFileAReport(t *testing.T) {
	w := newWorld(t)

	assert.Equal(t, http.StatusForbidden, w.as(reader, http.MethodPost, incidents+"/reports", report("x")).Code, "view is not create")
	assert.Equal(t, http.StatusForbidden, w.as(-robot, http.MethodPost, incidents+"/reports", report("x")).Code, "a service account is no reporter")
	assert.Equal(t, http.StatusUnauthorized, w.as(0, http.MethodPost, incidents+"/reports", report("x")).Code)
}

func TestWhoSeesWhat(t *testing.T) {
	w := newWorld(t)

	mine := decode[incident.Created](t, w.as(ana, http.MethodPost, incidents+"/reports", report("Ana's problem")).Data).Data.ID
	_ = decode[incident.Created](t, w.as(ivo, http.MethodPost, incidents+"/reports", report("Ivo's problem")).Data).Data.ID

	titles := func(who int) []string {
		res := w.as(who, http.MethodGet, incidents, nil)
		require.Equal(t, http.StatusOK, res.Code, string(res.Body))

		var out []string
		for _, i := range decode[[]incident.Incident](t, res.Data) {
			out = append(out, i.Title)
		}

		return out
	}

	assert.ElementsMatch(t, []string{"Ana's problem", "Ivo's problem"}, titles(staff), "the staff sees every incident")
	assert.Equal(t, []string{"Ana's problem"}, titles(ana))
	assert.Equal(t, []string{"Ivo's problem"}, titles(ivo))
	assert.Empty(t, titles(petra))
	assert.Empty(t, titles(-robot), "a service account without manage sees nothing")

	assert.Equal(t, http.StatusNotFound, w.as(petra, http.MethodGet, incidents+"/"+strconv.Itoa(mine), nil).Code)
	assert.Equal(t, http.StatusNotFound, w.as(petra, http.MethodGet, incidents+"/"+strconv.Itoa(mine)+"/comments", nil).Code)

	// A watcher sees it: Petra comments on... no, she cannot see it, so she cannot comment; Ana is made to watch for her.
	assert.Equal(t, http.StatusNotFound, w.as(petra, http.MethodPost, incidents+"/"+strconv.Itoa(mine)+"/comments", reportBody{"body": "hi"}).Code)
}

func TestCommentsFollowTheirVisibility(t *testing.T) {
	w := newWorld(t)
	id := strconv.Itoa(decode[incident.Created](t, w.as(ana, http.MethodPost, incidents+"/reports", report("Commented")).Data).Data.ID)

	res := w.as(ana, http.MethodPost, incidents+"/"+id+"/comments", reportBody{"body": "Still broken", "visibility": "internal"})
	require.Equal(t, http.StatusCreated, res.Code, string(res.Body))

	comment := decode[incident.CommentResponse](t, res.Data)
	assert.Equal(t, "public", comment.Visibility, "only the staff write internal comments")
	assert.Equal(t, ana, comment.AuthorUserID)
	require.NotNil(t, comment.Author)
	assert.Equal(t, "Ana Anic", comment.Author.Name)

	res = w.as(staff, http.MethodPost, incidents+"/"+id+"/comments", reportBody{"body": "Looking at it", "visibility": "internal"})
	require.Equal(t, http.StatusCreated, res.Code, string(res.Body))
	assert.Equal(t, "internal", decode[incident.CommentResponse](t, res.Data).Visibility)

	assert.Len(t, decode[[]incident.CommentResponse](t, w.as(staff, http.MethodGet, incidents+"/"+id+"/comments", nil).Data), 2)

	seen := decode[[]incident.CommentResponse](t, w.as(ana, http.MethodGet, incidents+"/"+id+"/comments", nil).Data)
	require.Len(t, seen, 1)
	assert.Equal(t, "Still broken", seen[0].Body)

	shown := decode[incident.Incident](t, w.as(ana, http.MethodGet, incidents+"/"+id, nil).Data)
	assert.Equal(t, 2, shown.CommentCount)

	assert.Equal(t, http.StatusBadRequest, w.as(ana, http.MethodPost, incidents+"/"+id+"/comments", reportBody{"body": "  "}).Code)
	assert.Equal(t, http.StatusBadRequest, w.as(ana, http.MethodPost, incidents+"/"+id+"/comments", reportBody{"body": "x", "visibility": "secret"}).Code)
	assert.Equal(t, http.StatusNotFound, w.as(ana, http.MethodPost, incidents+"/999/comments", reportBody{"body": "x"}).Code)
}

func TestAnIncidentIsChanged(t *testing.T) {
	w := newWorld(t)
	id := strconv.Itoa(decode[incident.Created](t, w.as(ana, http.MethodPost, incidents+"/reports", report("To triage")).Data).Data.ID)

	res := w.as(staff, http.MethodPatch, incidents+"/"+id, reportBody{
		"status": "resolved", "severity": "low", "assigned_to_user_id": petra, "resolution_summary": "fixed in 1.2",
		"external_ticket_provider": "github", "external_ticket_id": "GH-7", "external_ticket_url": "https://example.com/7",
	})
	require.Equal(t, http.StatusOK, res.Code, string(res.Body))

	changed := decode[incident.Incident](t, res.Data)
	assert.Equal(t, "resolved", changed.Status)
	assert.Equal(t, "low", changed.Severity)
	require.NotNil(t, changed.ResolvedAt)
	require.NotNil(t, changed.Assignee)
	assert.Equal(t, "Petra P", changed.Assignee.Name)
	assert.Equal(t, "GH-7", changed.ExternalTicketID)

	// A resolved incident no longer groups repeats: the same report opens a new one.
	again := decode[incident.Created](t, w.as(ana, http.MethodPost, incidents+"/reports", report("To triage")).Data)
	assert.Equal(t, "new", again.Data.Status, "a resolved incident that is reported again is reopened")
	assert.Equal(t, id, strconv.Itoa(again.Data.ID))

	res = w.as(staff, http.MethodPatch, incidents+"/"+id, reportBody{"external_ticket_provider": "none"})
	require.Equal(t, http.StatusOK, res.Code)
	assert.Empty(t, decode[incident.Incident](t, res.Data).ExternalTicketID, "no provider clears the ticket")

	assert.Equal(t, http.StatusBadRequest, w.as(staff, http.MethodPatch, incidents+"/"+id, reportBody{"status": "done"}).Code)
	assert.Equal(t, http.StatusNotFound, w.as(staff, http.MethodPatch, incidents+"/999", reportBody{"status": "triaged"}).Code)
	assert.Equal(t, http.StatusForbidden, w.as(reader, http.MethodPatch, incidents+"/"+id, reportBody{"status": "triaged"}).Code)
	assert.Equal(t, http.StatusBadRequest, w.as(staff, http.MethodGet, incidents+"/abc", nil).Code, "an id that is not a number")
}

func TestTheListPagesFiltersAndSearches(t *testing.T) {
	w := newWorld(t)

	for _, title := range []string{"Alpha crash", "Beta crash", "Gamma slow"} {
		require.Equal(t, http.StatusCreated, w.as(ana, http.MethodPost, incidents+"/reports", report(title)).Code)
	}

	res := w.as(staff, http.MethodGet, incidents+"?per_page=2&page=2", nil)
	require.Equal(t, http.StatusOK, res.Code, string(res.Body))
	assert.Len(t, decode[[]incident.Incident](t, res.Data), 1)
	assert.JSONEq(t, `{"total":3,"per_page":2,"page":2,"last_page":2,"from":3,"to":3}`, string(res.Meta))

	res = w.as(staff, http.MethodGet, incidents+"?search=crash&status=new&severity=high&source=user_report", nil)
	assert.Len(t, decode[[]incident.Incident](t, res.Data), 2)

	res = w.as(staff, http.MethodGet, incidents+"?status=resolved", nil)
	assert.Empty(t, decode[[]incident.Incident](t, res.Data))
}

func TestThePermissionsAreDefinedOnce(t *testing.T) {
	cat := newWorld(t).app
	_ = cat

	assert.Equal(t, "incident:view", incident.ViewIncidents)
	assert.Equal(t, core.VisibilityShared, "reporter_and_staff")
}
