//nolint:forcetypeassert,lll,paralleltest,varnamelen // tests in the style of the package's others
package incident_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/incident"
)

func occurrencesOf(id any) string { return incidents + "/" + toString(id) + "/occurrences" }

func toString(id any) string {
	if n, ok := id.(int); ok {
		return strconv.Itoa(n)
	}

	return fmt.Sprint(id)
}

// idOf is the id of an incident row.
func idOf(row map[string]any) int { return int(row["id"].(int64)) }

func TestTheStaffReadsTheOccurrencesOfAnIncidentNewestFirst(t *testing.T) {
	w := newWorld(t)

	for i := range 3 {
		w.now = w.now.Add(time.Minute)
		w.as(ana, http.MethodGet, prefix+"/v1/test/boom?i="+strconv.Itoa(i), nil, "X-Request-ID", "req-"+strconv.Itoa(i))
	}

	id := idOf(w.rows("incidents")[0])

	res := w.as(staff, http.MethodGet, occurrencesOf(id)+"?per_page=2", nil)
	require.Equal(t, http.StatusOK, res.Code, string(res.Body))

	page := decode[[]incident.OccurrenceResponse](t, res.Data)
	require.Len(t, page, 2, "the page size applies")
	assert.Equal(t, "req-2", page[0].RequestID, "newest first")
	assert.Equal(t, "req-1", page[1].RequestID)
	assert.Equal(t, "database exploded for user 42", page[0].Message)
	assert.Equal(t, "GET", page[0].Method)
	assert.Equal(t, "/api/v1/test/boom", page[0].Route)
	assert.Equal(t, ana, *page[0].UserID)
	require.NotNil(t, page[0].User, "the person is named")
	assert.Equal(t, ana, page[0].User.ID)
	assert.JSONEq(t, `{"kind":"user","id":2}`, string(mustJSON(t, asJSON(t, page[0].Context)["principal"])))
	assert.Contains(t, asJSON(t, page[0].Request), "headers", "the request is a document, not a string")
	assert.Contains(t, string(res.Meta), `"total":3`)

	second := decode[[]incident.OccurrenceResponse](t, w.as(staff, http.MethodGet, occurrencesOf(id)+"?per_page=2&page=2", nil).Data)
	require.Len(t, second, 1)
	assert.Equal(t, "req-0", second[0].RequestID)
}

func TestOnlyManagersReadOccurrences(t *testing.T) {
	w := newWorld(t)
	w.as(ana, http.MethodGet, prefix+"/v1/test/boom", nil)
	id := idOf(w.rows("incidents")[0])

	for _, who := range []int{ana, reader, -robot} {
		assert.Equal(t, http.StatusForbidden, w.as(who, http.MethodGet, occurrencesOf(id), nil).Code, "person %d", who)
	}

	assert.Equal(t, http.StatusUnauthorized, w.as(0, http.MethodGet, occurrencesOf(id), nil).Code)
}

func TestOccurrencesOfAnotherIncidentAreNotMixedInAndAnUnknownOneIsNotFound(t *testing.T) {
	w := newWorld(t)
	w.as(ana, http.MethodGet, prefix+"/v1/test/boom", nil, "X-Tenant", "7")
	w.as(ana, http.MethodGet, prefix+"/v1/test/panic", nil, "X-Tenant", "8")

	rows := w.rows("incidents")
	require.Len(t, rows, 2)

	for _, row := range rows {
		got := decode[[]incident.OccurrenceResponse](t, w.as(staff, http.MethodGet, occurrencesOf(idOf(row)), nil).Data)
		require.Len(t, got, 1)
		assert.EqualValues(t, row["id"], got[0].IncidentID)
	}

	assert.Equal(t, http.StatusNotFound, w.as(staff, http.MethodGet, occurrencesOf(9999), nil).Code)
	assert.Equal(t, http.StatusBadRequest, w.as(staff, http.MethodGet, occurrencesOf("abc"), nil).Code)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()

	raw, err := json.Marshal(value)
	require.NoError(t, err)

	return raw
}
