//nolint:goconst,paralleltest,thelper,varnamelen // tests in the style of the package's others
package notification_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/database/dbtest"
	"gorm.io/gorm"
)

// The inbox, the settings, the devices, the live stream, the test notification and the failed events are a person's: a
// service account, even one that holds every permission, is refused with 403.
func TestAServiceAccountIsRefusedWhatIsAPersons(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newWorld(t, db, authztest.AllowAll()).start()

		asRobot := func(method, path string) int {
			req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
			req.Header.Set("X-Robot", "5")

			rec := httptest.NewRecorder()
			w.handler.ServeHTTP(rec, req)

			return rec.Code
		}

		for _, c := range []struct{ method, path string }{
			{"GET", "/v1/notifications"}, {"GET", "/v1/notifications/unread"}, {"POST", "/v1/notifications/1/read"},
			{"POST", "/v1/notifications/read"}, {"GET", "/v1/notifications/stream"}, {"POST", "/v1/notifications/test"},
			{"GET", "/v1/notifications/preferences"}, {"PUT", "/v1/notifications/quiet-hours"},
			{"GET", "/v1/notifications/push"}, {"GET", "/v1/notifications/devices"}, {"DELETE", "/v1/notifications/devices/1"},
			{"GET", "/v1/events/dead-letters"}, {"GET", "/v1/events/consumers"},
		} {
			assert.Equal(t, http.StatusForbidden, asRobot(c.method, c.path), c.method+" "+c.path)
		}

		// the same person's request is served
		require.Equal(t, http.StatusOK, w.do(1, "GET", "/v1/notifications/unread", nil).Code)
	})
}
