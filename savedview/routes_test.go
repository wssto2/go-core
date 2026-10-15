//nolint:goconst,lll,paralleltest,varnamelen // tests in the style of the package's others
package savedview_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The wire is the first application's: {id, list_key, name, state, updated_at}, the state an object, no user_id.
func TestAViewIsSavedAndListedOnTheWire(t *testing.T) {
	w := newWorld(t)

	res := w.save(ana, "  New Clio ", `{"filters":{"brand":"10"},"search":""}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body)
	assert.JSONEq(t, `{"id":1,"list_key":"vehicles.catalogue","name":"New Clio","state":{"filters":{"brand":"10"},"search":""},"updated_at":"2026-03-04T09:00:00Z"}`, string(res.Data),
		"the name is trimmed, the time is UTC to the second as a read shows it")

	list := w.list(ana, catalogue)
	require.Equal(t, http.StatusOK, list.Code, list.Body)
	assert.JSONEq(t, `[`+string(res.Data)+`]`, string(list.Data))
	assert.Contains(t, list.Body, `"data":[{"id":1,"list_key":"vehicles.catalogue","name":"New Clio","state":{"filters":{"brand":"10"},"search":""},"updated_at":"2026-03-04T09:00:00Z"}]`,
		"the fields are in the order the first application wrote them")

	empty := w.list(ana, "orders.list")
	assert.JSONEq(t, `{"success":true,"data":[]}`, empty.Body, "a list without views is an empty array")
}

func TestSavingUnderTheSameNameReplacesTheState(t *testing.T) {
	w := newWorld(t)

	first := w.save(ana, "Clio", `{"search":"clio"}`)
	require.Equal(t, http.StatusOK, first.Code, first.Body)

	w.now = w.now.Add(time.Hour)

	second := w.save(ana, "Clio", `{"search":"megane"}`)
	require.Equal(t, http.StatusOK, second.Code, second.Body)
	assert.JSONEq(t, `{"id":1,"list_key":"vehicles.catalogue","name":"Clio","state":{"search":"megane"},"updated_at":"2026-03-04T10:00:00Z"}`, string(second.Data))
	assert.Len(t, decodeList(t, w.list(ana, catalogue)), 1)
}

func TestTheStateMustBeAJSONObject(t *testing.T) {
	w := newWorld(t)

	for _, state := range []string{"[]", "not json", `"string"`, "null", " null ", "5"} {
		res := w.save(ana, "x", state)
		assert.Equal(t, http.StatusBadRequest, res.Code, state+": "+res.Body)
		assert.JSONEq(t, `{"error":"state must be a JSON object","success":false}`, res.Body, state)
	}

	assert.Empty(t, decodeList(t, w.list(ana, catalogue)))
}

func TestTheInputsAreBounded(t *testing.T) {
	w := newWorld(t)

	long := func(n int) string { return strings.Repeat("x", n) }
	body := func(list, name, state any) map[string]any {
		return map[string]any{"list_key": list, "name": name, "state": state}
	}

	for name, c := range map[string]struct {
		body map[string]any
		want int
	}{
		"no list":         {map[string]any{"name": "x", "state": "{}"}, http.StatusUnprocessableEntity},
		"no name":         {map[string]any{"list_key": catalogue, "state": "{}"}, http.StatusUnprocessableEntity},
		"a blank name":    {body(catalogue, "   ", "{}"), http.StatusUnprocessableEntity},
		"a blank list":    {body("  ", "x", "{}"), http.StatusUnprocessableEntity},
		"no state":        {map[string]any{"list_key": catalogue, "name": "x"}, http.StatusUnprocessableEntity},
		"a long list":     {body(long(101), "x", "{}"), http.StatusUnprocessableEntity},
		"a long name":     {body(catalogue, long(61), "{}"), http.StatusUnprocessableEntity},
		"a long state":    {body(catalogue, "x", `{"s":"`+long(4000)+`"}`), http.StatusUnprocessableEntity},
		"an object":       {body(catalogue, "x", map[string]any{"a": 1}), http.StatusUnprocessableEntity},
		"the list bound":  {body(long(100), "x", "{}"), http.StatusOK},
		"the name bound":  {body(catalogue, long(60), "{}"), http.StatusOK},
		"the state bound": {body(catalogue, "y", `{"s":"`+long(3992)+`"}`), http.StatusOK},
	} {
		res := w.as(ana, http.MethodPost, views, c.body)
		assert.Equal(t, c.want, res.Code, name+": "+res.Body)
	}

	assert.Equal(t, http.StatusBadRequest, w.as(ana, http.MethodPost, views, "{not json").Code)

	missing := w.as(ana, http.MethodGet, views, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, missing.Code)
	assert.JSONEq(t, `{"debug_errors":{"list":["required"]},"errors":{"list":["Validation failed: required"]},"message":"validation failed","success":false}`, missing.Body)
	assert.Equal(t, http.StatusUnprocessableEntity, w.list(ana, long(101)).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, w.list(ana, "%20%20").Code, "a blank list")
}

func TestAtMostTwentyViewsPerList(t *testing.T) {
	w := newWorld(t)

	for i := range 20 {
		res := w.save(ana, "view "+strconv.Itoa(i), "{}")
		require.Equal(t, http.StatusOK, res.Code, res.Body)
	}

	over := w.save(ana, "one too many", "{}")
	assert.Equal(t, http.StatusBadRequest, over.Code)
	assert.JSONEq(t, `{"error":"at most 20 saved views per list","success":false}`, over.Body)

	assert.Equal(t, http.StatusOK, w.save(ana, "view 0", `{"search":"x"}`).Code, "replacing one is allowed at the cap")
	assert.Equal(t, http.StatusOK, w.as(ana, http.MethodPost, views, map[string]any{"list_key": "orders.list", "name": "other list", "state": "{}"}).Code)
	assert.Equal(t, http.StatusOK, w.save(ivo, "another person", "{}").Code)
	assert.Len(t, decodeList(t, w.list(ana, catalogue)), 20)
}

func TestAViewIsTheSessionPersonsAlone(t *testing.T) {
	w := newWorld(t)

	// A body that names another person is ignored: the person is the session's.
	res := w.as(ana, http.MethodPost, views, map[string]any{"list_key": catalogue, "name": "mine", "state": "{}", "user_id": ivo})
	require.Equal(t, http.StatusOK, res.Code, res.Body)
	assert.NotContains(t, res.Body, "user_id", "the owner is not on the wire")
	assert.Empty(t, decodeList(t, w.list(ivo, catalogue)))
	assert.Len(t, decodeList(t, w.list(ana, catalogue)), 1)

	// The same name of another person is another view.
	theirs := w.save(ivo, "mine", `{"search":"theirs"}`)
	require.Equal(t, http.StatusOK, theirs.Code, theirs.Body)
	assert.JSONEq(t, `{"search":"theirs"}`, string(decodeList(t, w.list(ivo, catalogue))[0].State))
	assert.JSONEq(t, `{}`, string(decodeList(t, w.list(ana, catalogue))[0].State))
}

func TestDeleteOfAnotherPersonsViewIsNotFound(t *testing.T) {
	w := newWorld(t)

	mine := w.save(ana, "mine", "{}")
	require.Equal(t, http.StatusOK, mine.Code, mine.Body)

	other := w.as(ivo, http.MethodDelete, views+"/1", nil)
	assert.Equal(t, http.StatusNotFound, other.Code)
	assert.JSONEq(t, `{"error":"saved view not found","success":false}`, other.Body)
	assert.Len(t, decodeList(t, w.list(ana, catalogue)), 1, "the view is still there")

	assert.Equal(t, http.StatusNotFound, w.as(ana, http.MethodDelete, views+"/77", nil).Code)

	own := w.as(ana, http.MethodDelete, views+"/1", nil)
	assert.Equal(t, http.StatusNoContent, own.Code, own.Body)
	assert.Empty(t, own.Body)
	assert.Empty(t, decodeList(t, w.list(ana, catalogue)))
	assert.Equal(t, http.StatusNotFound, w.as(ana, http.MethodDelete, views+"/1", nil).Code, "the second delete")
}

func TestAMalformedOrNonPositiveIDIsRefused(t *testing.T) {
	w := newWorld(t)

	bad := w.as(ana, http.MethodDelete, views+"/abc", nil)
	assert.Equal(t, http.StatusBadRequest, bad.Code, "a path that is not a number")
	assert.JSONEq(t, `{"error":"invalid id","success":false}`, bad.Body)

	zero := w.as(ana, http.MethodDelete, views+"/0", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, zero.Code, "a zero id is a validation failure")
	assert.JSONEq(t, `{"debug_errors":{"id":["path"]},"errors":{"id":["Validation failed: must_be_positive_number"]},"message":"validation failed","success":false}`, zero.Body)

	assert.Equal(t, http.StatusUnprocessableEntity, w.as(ana, http.MethodDelete, views+"/-3", nil).Code)
}

// Nobody signed in is 401; a service account, whatever it holds, is refused: saved views are a person's.
func TestOnlyASignedInPersonKeepsViews(t *testing.T) {
	w := newWorld(t)

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, views + "?list=" + catalogue}, {http.MethodPost, views}, {http.MethodDelete, views + "/1"},
	} {
		none := w.as(0, c.method, c.path, `{"list_key":"a","name":"b","state":"{}"}`)
		assert.Equal(t, http.StatusUnauthorized, none.Code, c.method+" "+c.path+": "+none.Body)

		service := w.as(-robot, c.method, c.path, `{"list_key":"a","name":"b","state":"{}"}`)
		assert.Equal(t, http.StatusForbidden, service.Code, c.method+" "+c.path+": "+service.Body)
	}
}

type viewJSON struct {
	ID    int             `json:"id"`
	Name  string          `json:"name"`
	State json.RawMessage `json:"state"`
}

// decodeList is the views an index answered with.
func decodeList(t *testing.T, res answer) []viewJSON {
	t.Helper()
	require.Equal(t, http.StatusOK, res.Code, res.Body)

	var out []viewJSON
	require.NoError(t, json.Unmarshal(res.Data, &out), res.Body)

	return out
}
