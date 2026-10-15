//nolint:exhaustruct,exhaustruct_v5,lll,paralleltest,thelper,varnamelen,gochecknoglobals,goconst,mnd,funlen,cyclop,maintidx // tests in the style of the package's others
package incident_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/incident"
	"github.com/wssto2/go-core/route"
)

func asJSON(t *testing.T, value any) map[string]any {
	t.Helper()

	switch typed := value.(type) {
	case string:
		var out map[string]any

		require.NoError(t, json.Unmarshal([]byte(typed), &out), typed)

		return out
	case []byte:
		var out map[string]any

		require.NoError(t, json.Unmarshal(typed, &out), string(typed))

		return out
	case map[string]any:
		return typed
	default:
		t.Fatalf("not JSON: %T", value)

		return nil
	}
}

func TestAServerErrorBecomesAnIncidentAndItsRepeatsAreCounted(t *testing.T) {
	w := newWorld(t, incident.Capture{Environment: "test"})

	res := w.as(ana, http.MethodGet, prefix+"/v1/test/boom?search=secret-name", nil, "X-Tenant", "7", "X-Request-ID", "req-1",
		"Authorization", "Bearer abc", "Cookie", "session=xyz", "X-Api-Token", "tok", "Accept", "application/json")
	assert.Equal(t, http.StatusInternalServerError, res.Code, "the answer is still the 500")

	rows := w.rows("incidents")
	require.Len(t, rows, 1)

	row := rows[0]
	assert.Equal(t, "runtime_error", row["source"])
	assert.Equal(t, "high", row["severity"])
	assert.Equal(t, "staff_only", row["visibility"])
	assert.Equal(t, "new", row["status"])
	assert.Equal(t, "test", row["environment"])
	assert.Equal(t, "test", row["affected_module"])
	assert.Equal(t, "/api/v1/test/boom", row["affected_route"])
	assert.Equal(t, "database exploded for user 42", row["error_message_sample"])
	assert.Equal(t, int64(1), row["occurrence_count"])
	assert.Contains(t, row["title"], "error in test")

	occurrences := w.rows("incident_occurrences")
	require.Len(t, occurrences, 1)

	occurrence := occurrences[0]
	assert.Equal(t, "GET", occurrence["request_method"])
	assert.EqualValues(t, ana, occurrence["user_id"])
	assert.EqualValues(t, 7, occurrence["tenant_id"])
	assert.NotEmpty(t, occurrence["request_id"])
	assert.Equal(t, "database exploded for user 42", occurrence["message"])
	assert.Equal(t, "database exploded for user {n}", occurrence["normalized_message"])

	request := asJSON(t, occurrence["request_json"])
	assert.Equal(t, "/api/v1/test/boom", request["path"])
	assert.EqualValues(t, 500, request["status_code"])

	headers := request["headers"].(map[string]any)
	assert.Equal(t, "<redacted>", headers["Authorization"])
	assert.Equal(t, "<redacted>", headers["Cookie"])
	assert.Equal(t, "<redacted>", headers["X-Api-Token"], "audit.Mask redacts a name that says token")
	assert.Equal(t, "application/json", headers["Accept"])
	assert.NotContains(t, occurrence["request_json"], "abc")

	assert.Equal(t, map[string]any{"search": []any{"secret-name"}}, request["query"], "the query is kept, masked by key")

	// The same error again, from another person: one incident, two occurrences.
	w.now = w.now.Add(time.Minute)
	w.as(ivo, http.MethodGet, prefix+"/v1/test/boom", nil)
	assert.Len(t, w.rows("incidents"), 1)
	assert.Len(t, w.rows("incident_occurrences"), 2)
	assert.EqualValues(t, 2, w.rows("incidents")[0]["occurrence_count"])

	// A 404 is not a server error.
	w.as(ana, http.MethodGet, prefix+"/v1/test/missing", nil)
	assert.Len(t, w.rows("incident_occurrences"), 2)
}

func TestTheStaffSeesTheCapturedIncidentAndNobodyElse(t *testing.T) {
	w := newWorld(t)
	w.as(ana, http.MethodGet, prefix+"/v1/test/boom", nil)

	list := func(who int) int {
		return len(decode[[]incident.IncidentResponse](t, w.as(who, http.MethodGet, incidents, nil).Data))
	}

	assert.Equal(t, 1, list(staff))
	assert.Equal(t, 0, list(ana), "a captured incident is staff only, even for the person it happened to")
}

func TestAPanicIsCapturedAndTheAnswerIsUnchanged(t *testing.T) {
	with := newWorld(t)
	got := with.as(ana, http.MethodGet, prefix+"/v1/test/panic", nil)

	// The same route in an application without the module answers the same.
	without := panicWithoutTheModule(t)

	assert.Equal(t, without.Code, got.Code)
	assert.Equal(t, http.StatusInternalServerError, got.Code)
	assert.JSONEq(t, string(without.Body), string(got.Body))

	rows := with.rows("incidents")
	require.Len(t, rows, 1, "a panic leaves an incident")
	assert.Contains(t, rows[0]["error_message_sample"], "index out of range")
	assert.Equal(t, "high", rows[0]["severity"])

	occurrence := with.rows("incident_occurrences")[0]
	assert.Equal(t, "panic", occurrence["error_code"])
	assert.Contains(t, occurrence["stacktrace"], "capture_test.go", "the stack is the panic's, from the line that panicked")

	// A second panic from the same line is the same incident: the fingerprint ignores addresses and goroutine numbers.
	with.as(ivo, http.MethodGet, prefix+"/v1/test/panic", nil)
	assert.Len(t, with.rows("incidents"), 1)
	assert.EqualValues(t, 2, with.rows("incidents")[0]["occurrence_count"])
}

// panicWithoutTheModule is the answer of the panicking route in an application that does not have the module.
func panicWithoutTheModule(t *testing.T) answer {
	t.Helper()

	reg, cleanup := database.NewTestRegistry("local")
	t.Cleanup(func() { _ = cleanup() })

	app := gocore.New(bootstrap.DefaultConfig(), gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithAuthentication(signIn), gocore.WithAuthorizer(permissions), gocore.WithPrefix(prefix))
	app.Routes(route.Get[route.None, route.Empty]("/v1/test/panic").Authenticated().To(func(context.Context, route.None) (route.Empty, error) {
		var nothing []int

		return route.Empty{}, errors.New(strconv.Itoa(nothing[3]))
	}))

	handler, err := app.Handler()
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, prefix+"/v1/test/panic", nil)
	req.Header.Set("X-User", strconv.Itoa(ana))

	rec := httptest.NewRecorder()
	withRecovery(handler).ServeHTTP(rec, req)

	return answer{Code: rec.Code, Body: rec.Body.Bytes()}
}

func TestAServiceAccountIsRecordedAsOneNotAsUserZero(t *testing.T) {
	w := newWorld(t)
	w.as(-robot, http.MethodGet, prefix+"/v1/test/boom", nil)

	occurrences := w.rows("incident_occurrences")
	require.Len(t, occurrences, 1)
	assert.Nil(t, occurrences[0]["user_id"], "a service account's id is not a user id")

	context := asJSON(t, occurrences[0]["context_json"])
	assert.Equal(t, map[string]any{"kind": "service", "id": float64(robot)}, context["principal"])
}

func TestTheTrailIsTheCallersLastCallsUnderThePrefix(t *testing.T) {
	w := newWorld(t, incident.Capture{TrailSize: 3})

	for i := range 5 {
		w.now = w.now.Add(time.Second)
		w.as(ana, http.MethodGet, prefix+"/v1/test/ok?q=name"+strconv.Itoa(i), nil, "Referer", "https://app.example.com/vehicles/"+strconv.Itoa(i))
	}

	w.as(ivo, http.MethodGet, prefix+"/v1/test/ok", nil)
	w.as(ana, http.MethodGet, "/not-the-api", nil)
	w.as(ana, http.MethodGet, prefix+"/v1/test/boom", nil)

	context := asJSON(t, w.rows("incident_occurrences")[0]["context_json"])
	recent := context["recent_requests"].([]any)
	require.Len(t, recent, 3, "the size bounds the trail")

	last := recent[2].(map[string]any)
	assert.Equal(t, "GET", last["method"])
	assert.Equal(t, prefix+"/v1/test/ok", last["path"], "the query string is left out")
	assert.EqualValues(t, 204, last["status"])
	assert.Equal(t, "/vehicles/4", last["page"])
	assert.Equal(t, "/vehicles/2", recent[0].(map[string]any)["page"], "the oldest of the last three")
}

func TestTheTrailCanBeTurnedOff(t *testing.T) {
	w := newWorld(t, incident.Capture{TrailSize: -1})
	w.as(ana, http.MethodGet, prefix+"/v1/test/ok", nil)
	w.as(ana, http.MethodGet, prefix+"/v1/test/boom", nil)

	context := asJSON(t, w.rows("incident_occurrences")[0]["context_json"])
	assert.Empty(t, context["recent_requests"])
}

func TestTheModuleOfARequestFollowsThePrefixAndTheVersion(t *testing.T) {
	w := newWorld(t)
	w.as(ana, http.MethodGet, prefix+"/v1/test/boom", nil)
	assert.Equal(t, "test", w.rows("incidents")[0]["affected_module"])

	named := newWorld(t, incident.Capture{Module: func(path string) string { return "custom:" + path }})
	named.as(ana, http.MethodGet, prefix+"/v1/test/boom", nil)
	assert.Equal(t, "custom:/api/v1/test/boom", named.rows("incidents")[0]["affected_module"])
}

func TestACaptureNeverBreaksTheRequest(t *testing.T) {
	w := newWorld(t)
	require.NoError(t, w.db.Exec("DROP TABLE incident_occurrences").Error)

	res := w.as(ana, http.MethodGet, prefix+"/v1/test/boom", nil)
	assert.Equal(t, http.StatusInternalServerError, res.Code, "the answer is the server error, not the capture's")
	assert.Equal(t, http.StatusNoContent, w.as(ana, http.MethodGet, prefix+"/v1/test/ok", nil).Code)
}

var _ gin.HandlerFunc = signIn
var _ authz.Authorizer = permissions
