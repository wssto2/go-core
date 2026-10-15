//nolint:testpackage // uses the package's twoChains and call helpers
package gocore

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestWithServiceWorkerServesTheFileAtTheRootOutsideThePrefix(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "worker.js")
	if err := os.WriteFile(file, []byte("self.addEventListener('fetch', () => {})"), 0o600); err != nil {
		t.Fatal(err)
	}

	app, _ := twoChains(t, nil)
	app.prefix = "/api"
	WithServiceWorker(file)(app)

	handler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}

	rec := call(handler, "/sw.js") // no sign-in: a worker script is fetched without credentials
	if rec.Code != http.StatusOK || rec.Body.String() != "self.addEventListener('fetch', () => {})" {
		t.Fatalf("%d %q", rec.Code, rec.Body)
	}

	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control %q", got)
	}

	if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type %q", got)
	}

	if rec := call(handler, "/api/sw.js"); rec.Code != http.StatusNotFound {
		t.Errorf("under the prefix: %d", rec.Code)
	}
}

func TestWithServiceWorkerMissingFileAnswers404(t *testing.T) {
	t.Parallel()

	app, _ := twoChains(t, nil)
	WithServiceWorker(filepath.Join(t.TempDir(), "absent.js"))(app)

	handler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}

	if rec := call(handler, "/sw.js"); rec.Code != http.StatusNotFound {
		t.Errorf("%d", rec.Code)
	}
}
