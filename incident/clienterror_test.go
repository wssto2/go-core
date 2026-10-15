//nolint:exhaustruct,exhaustruct_v5,goconst,lll,paralleltest,varnamelen // tests in the style of the package's others
package incident_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/incident"
	"github.com/wssto2/go-core/ratelimit"
)

const clientErrors = incidents + "/client-errors"

func browserError(message, frame string) reportBody {
	return reportBody{
		"message": message, "stack": "Error: " + message + "\n    at " + frame, "route": "/vehicles/12",
		"browser": "Firefox 130", "app_version": "1.4.2", "request_ids": []string{"req-a", "req-b"},
	}
}

func TestABrowserErrorBecomesALowSeverityIncidentOfItsOwn(t *testing.T) {
	w := newWorld(t, incident.Capture{Environment: "test"})

	res := w.as(ana, http.MethodPost, clientErrors, browserError("Cannot read properties of undefined (reading 'id')", "render (https://app.example.com/assets/index-3f9a.js:1:4521)"), "X-Tenant", "7")
	assert.Equal(t, http.StatusNoContent, res.Code, string(res.Body))
	assert.Empty(t, res.Body, "there is nothing to read back")

	rows := w.rows("incidents")
	require.Len(t, rows, 1)
	assert.Equal(t, "client_error", rows[0]["source"])
	assert.Equal(t, "low", rows[0]["severity"])
	assert.Equal(t, "staff_only", rows[0]["visibility"])
	assert.Equal(t, "/vehicles/12", rows[0]["affected_route"])
	assert.Equal(t, "test", rows[0]["environment"])
	assert.Equal(t, "Browser error: Cannot read properties of undefined (reading 'id')", rows[0]["title"])

	occurrence := w.rows("incident_occurrences")[0]
	assert.EqualValues(t, ana, occurrence["user_id"])
	assert.EqualValues(t, 7, occurrence["tenant_id"])

	context := asJSON(t, occurrence["context_json"])
	assert.Equal(t, "Firefox 130", context["browser"])
	assert.Equal(t, "1.4.2", context["app_version"])
	assert.Equal(t, []any{"req-a", "req-b"}, context["request_ids"])

	// The staff reads it with the other incidents, and the person who reported it does not.
	assert.Len(t, decode[[]incident.Incident](t, w.as(staff, http.MethodGet, incidents+"?source=client_error", nil).Data), 1)
	assert.Empty(t, decode[[]incident.Incident](t, w.as(ana, http.MethodGet, incidents, nil).Data))
}

func TestBrowserErrorsAreGroupedByMessageAndTopFrame(t *testing.T) {
	w := newWorld(t)
	message := "Cannot read properties of undefined (reading 'id')"

	// The same error from another line and column of the same build, and from another person.
	w.as(ana, http.MethodPost, clientErrors, browserError(message, "render (https://app.example.com/assets/index-3f9a.js:1:4521)"))
	w.as(ivo, http.MethodPost, clientErrors, browserError(message, "render (https://app.example.com/assets/index-3f9a.js:1:4890)"))
	assert.Len(t, w.rows("incidents"), 1)
	assert.EqualValues(t, 2, w.rows("incidents")[0]["occurrence_count"])

	// Another frame, and another message, are other incidents.
	w.as(ana, http.MethodPost, clientErrors, browserError(message, "other (https://app.example.com/assets/index-3f9a.js:1:12)"))
	w.as(ana, http.MethodPost, clientErrors, browserError("x is not a function", "render (https://app.example.com/assets/index-3f9a.js:1:4521)"))
	assert.Len(t, w.rows("incidents"), 3)

	// Firefox writes its frames as fn@file.
	w.as(ana, http.MethodPost, clientErrors, reportBody{"message": "boom", "stack": "render@https://app.example.com/a.js:3:4\nmain@https://app.example.com/a.js:9:9"})
	w.as(ana, http.MethodPost, clientErrors, reportBody{"message": "boom", "stack": "render@https://app.example.com/a.js:30:40"})
	assert.Len(t, w.rows("incidents"), 4)

	// A closed incident that is seen again is reopened, and is not announced as new.
	id := w.rows("incidents")[0]["id"]
	w.as(staff, http.MethodPatch, incidents+"/"+fmt.Sprint(id), reportBody{"status": "resolved"})
	w.as(ana, http.MethodPost, clientErrors, browserError(message, "render (https://app.example.com/assets/index-3f9a.js:1:1)"))
	assert.Len(t, w.rows("incidents"), 4)
	assert.Equal(t, "new", w.rows("incidents")[0]["status"])
	assert.EqualValues(t, 3, w.rows("incidents")[0]["occurrence_count"])
}

func TestABrowserErrorReportIsBoundedAndNeverAServerError(t *testing.T) {
	w := newWorld(t)

	for name, body := range map[string]any{
		"no message":       reportBody{"stack": "x"},
		"a blank message":  reportBody{"message": "   "},
		"a long message":   reportBody{"message": strings.Repeat("m", 2001)},
		"a long stack":     reportBody{"message": "m", "stack": strings.Repeat("s", 20001)},
		"a long route":     reportBody{"message": "m", "route": strings.Repeat("r", 256)},
		"a long browser":   reportBody{"message": "m", "browser": strings.Repeat("b", 501)},
		"a long version":   reportBody{"message": "m", "app_version": strings.Repeat("v", 101)},
		"too many request": reportBody{"message": "m", "request_ids": make([]string, 21)},
		"the wrong types":  reportBody{"message": 12, "stack": []int{1}},
	} {
		res := w.as(ana, http.MethodPost, clientErrors, body)
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, res.Code, name+": "+string(res.Body))
	}

	for name, raw := range map[string]string{"not JSON": "{nope", "an array": "[1,2]", "null": "null", "empty": ""} {
		res := w.as(ana, http.MethodPost, clientErrors, raw)
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, res.Code, name+": "+string(res.Body))
	}

	assert.Empty(t, w.rows("incidents"))

	// A request id that is too long is cut, and so is an emoji the tables cannot store.
	res := w.as(ana, http.MethodPost, clientErrors, reportBody{"message": "smile 😀 failed", "request_ids": []string{strings.Repeat("i", 300), ""}})
	assert.Equal(t, http.StatusNoContent, res.Code, string(res.Body))
	assert.Equal(t, "smile � failed", w.rows("incidents")[0]["error_message_sample"])
	assert.Len(t, asJSON(t, w.rows("incident_occurrences")[0]["context_json"])["request_ids"], 1)
}

func TestOnlyASignedInPersonReportsABrowserError(t *testing.T) {
	w := newWorld(t)

	assert.Equal(t, http.StatusUnauthorized, w.as(0, http.MethodPost, clientErrors, browserError("x", "f (a.js:1:1)")).Code)
	assert.Equal(t, http.StatusForbidden, w.as(-robot, http.MethodPost, clientErrors, browserError("x", "f (a.js:1:1)")).Code)
	assert.Equal(t, http.StatusNoContent, w.as(reader, http.MethodPost, clientErrors, browserError("x", "f (a.js:1:1)")).Code, "any signed-in person, whatever they hold")
}

func TestAPersonsBrowserErrorsAreRateLimited(t *testing.T) {
	limiter := ratelimit.NewInMemoryLimiter(2, time.Hour)
	t.Cleanup(limiter.Stop)

	w := newWorld(t, incident.ClientErrors(limiter))

	for i := range 4 {
		res := w.as(ana, http.MethodPost, clientErrors, browserError("same", "f (a.js:1:"+strings.Repeat("9", i+1)+")"))
		assert.Equal(t, http.StatusNoContent, res.Code, "an excess report is dropped, not refused")
	}

	assert.EqualValues(t, 2, w.rows("incidents")[0]["occurrence_count"], "the two over the limit were dropped")
	assert.Equal(t, http.StatusNoContent, w.as(ivo, http.MethodPost, clientErrors, browserError("same", "f (a.js:1:1)")).Code)
	assert.EqualValues(t, 3, w.rows("incidents")[0]["occurrence_count"], "another person has a limit of their own")
}
