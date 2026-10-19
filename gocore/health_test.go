//nolint:testpackage // uses the package's runnable and serving helpers
package gocore

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// /health and /ready are part of the application, however it is entered: Run (the deploy checklist's probes) and
// Handler (tests).
func TestHealthAndReadyAreMountedThroughRunAndHandler(t *testing.T) {
	t.Parallel()

	app, port := runnable(t)
	base := serving(t, app, port)

	for _, path := range []string{"/health", "/ready"} {
		resp, err := http.Get(base + path) //nolint:noctx // a test
		if err != nil {
			t.Fatal(err)
		}

		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Run: GET %s = %d", path, resp.StatusCode)
		}
	}

	handler, err := testApp(t, "local").Handler()
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/health", "/ready"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

		if w.Code != http.StatusOK {
			t.Errorf("Handler: GET %s = %d", path, w.Code)
		}
	}
}
