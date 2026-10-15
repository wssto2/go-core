//nolint:exhaustruct,exhaustruct_v5,lll,paralleltest,thelper,varnamelen,gochecknoglobals,goconst,mnd,funlen,cyclop,maintidx // tests in the style of the package's others
package incident_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/incident"
)

func TestOldOccurrencesGoExceptTheNewestOfEachIncident(t *testing.T) {
	w := newWorld(t)

	// Three sightings of one incident 100, 99 and 98 days ago, and one of another 100 days ago, one recent.
	hit := func(path string, daysAgo int) {
		w.now = time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC).Add(-time.Duration(daysAgo) * 24 * time.Hour)
		w.as(ana, http.MethodGet, prefix+path, nil)
	}

	hit("/v1/test/boom", 100)
	hit("/v1/test/boom", 99)
	hit("/v1/test/boom", 98)
	hit("/v1/test/panic", 100)
	hit("/v1/test/missing", 0)

	w.now = time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)
	require.Len(t, w.rows("incident_occurrences"), 4)

	swept, err := w.inc.Sweep(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, swept.Occurrences, "the two older ones of the first incident")
	assert.Len(t, w.rows("incident_occurrences"), 2, "the newest of each incident stays, however old")

	swept, err = w.inc.Sweep(t.Context())
	require.NoError(t, err)
	assert.Zero(t, swept.Occurrences)
}

func TestTheRetentionIsAnOption(t *testing.T) {
	w := newWorld(t, incident.Retain(time.Hour))

	for range 2 {
		w.now = w.now.Add(-2 * time.Hour)
		w.as(ana, http.MethodGet, prefix+"/v1/test/boom", nil)
	}

	w.now = w.now.Add(4 * time.Hour)

	swept, err := w.inc.Sweep(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, swept.Occurrences)
}
