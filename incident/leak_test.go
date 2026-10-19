//nolint:exhaustruct,exhaustruct_v5,lll,paralleltest,varnamelen // tests in the style of the package's others
package incident_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// secrets are what a request might carry that no occurrence may keep.
var secrets = []string{"hunter2-body", "hunter2-field", "tok-in-path", "Bearer abc", "session=xyz", "abc-key", "client-secret-header"}

// storedText is every text column of every occurrence that a reader could see.
func storedText(w *world) string {
	text := ""

	for _, row := range w.rows("incident_occurrences") {
		for _, column := range []string{"message", "stacktrace", "context_json", "request_json", "affected_route", "request_id"} {
			text += fmt.Sprint(" ", row[column])
		}
	}

	return text
}

func TestAServerErrorOccurrenceStoresNoSecret(t *testing.T) {
	w := newWorld(t)

	res := w.as(ana, http.MethodPost, prefix+"/v1/test/leak/tok-in-path?password=hunter2-body&q=1",
		map[string]string{"password": "hunter2-body"},
		"Authorization", "Bearer abc", "Cookie", "session=xyz", "X-Api-Key", "abc-key", "X-Client-Secret", "client-secret-header")
	require.Equal(t, http.StatusInternalServerError, res.Code, string(res.Body))
	require.Len(t, w.rows("incident_occurrences"), 1)

	stored := storedText(w)
	for _, secret := range secrets {
		assert.NotContains(t, stored, secret)
	}
}

func TestABrowserErrorOccurrenceStoresNoSecret(t *testing.T) {
	w := newWorld(t)

	body := browserError("Cannot read properties of undefined (reading 'id')", "render (https://app.example.com/a.js:1:2)")
	body["password"], body["detail"] = "hunter2-body", "mount"
	res := w.as(ana, http.MethodPost, clientErrors, body, "Authorization", "Bearer abc", "Cookie", "session=xyz")
	require.Equal(t, http.StatusNoContent, res.Code, string(res.Body))
	require.Len(t, w.rows("incident_occurrences"), 1)

	stored := storedText(w)
	for _, secret := range secrets {
		assert.NotContains(t, stored, secret)
	}
}
