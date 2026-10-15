//nolint:testpackage // uses the package's twoChains and call helpers
package gocore

import (
	"net/http"
	"strings"
	"testing"
)

const apiPrefix = "/api"

func TestRootServesOutsideThePrefixBehindTheAppsSecurity(t *testing.T) {
	t.Parallel()

	app, _ := twoChains(t, nil)
	app.prefix = apiPrefix
	app.Routes(okRoute("/inside").To(serve("inside")))
	app.Root(okRoute("/outside").To(serve("outside")), okRoute("/open").Public().To(serve("open")))

	handler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}

	for _, scenario := range []struct {
		path    string
		headers []string
		status  int
	}{
		{"/outside", []string{sessionHdr, "ok"}, http.StatusOK},
		{"/outside", nil, http.StatusUnauthorized},
		{"/api/outside", []string{sessionHdr, "ok"}, http.StatusNotFound},
		{"/open", nil, http.StatusOK},
		{"/inside", []string{sessionHdr, "ok"}, http.StatusNotFound},
		{"/api/inside", []string{sessionHdr, "ok"}, http.StatusOK},
	} {
		if rec := call(handler, scenario.path, scenario.headers...); rec.Code != scenario.status {
			t.Errorf("%s %v: %d %s, want %d", scenario.path, scenario.headers, rec.Code, rec.Body, scenario.status)
		}
	}
}

func TestRootAndPrefixedRoutesDoNotClash(t *testing.T) {
	t.Parallel()

	app, _ := twoChains(t, nil)
	app.prefix = apiPrefix
	app.Routes(okRoute("/same").To(serve("inside")))
	app.Root(okRoute("/same").To(serve("outside")))

	err := app.Check()
	if err != nil {
		t.Fatalf("different paths once the prefix is counted: %v", err)
	}

	app.Root(okRoute("/same").To(serve("again")))

	err = app.Check()
	if err == nil || !strings.Contains(err.Error(), "installed twice") {
		t.Fatalf("two root routes on one path must clash, got %v", err)
	}
}
