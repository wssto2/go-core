//nolint:paralleltest,lll,noinlineerr,varnamelen // tests in the style of the package's others
package route_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/route"
)

func serveOK(context.Context, route.None) (string, error) { return "ok", nil }

func statusWith(t *testing.T, r route.Bound, held ...string) int {
	t.Helper()

	e := newEngine()
	if err := r.Mount(e, secured(authztest.DenyAll().Allow(held...))); err != nil {
		t.Fatal(err)
	}

	return do(e, http.MethodGet, "/x", "").Code
}

func TestRequiresAnyPassesWithEitherAndRefusesWithNeither(t *testing.T) {
	r := route.Get[route.None, string]("/x").RequiresAny("a:view", "b:view").To(serveOK)

	for held, want := range map[string]int{"a:view": 200, "b:view": 200, "c:view": 403} {
		if got := statusWith(t, r, held); got != want {
			t.Fatalf("holding %s: %d, want %d", held, got, want)
		}
	}

	if got := statusWith(t, r); got != http.StatusForbidden {
		t.Fatalf("holding nothing: %d", got)
	}
}

func TestRequiresAllNeedsEvery(t *testing.T) {
	r := route.Get[route.None, string]("/x").RequiresAll("a:view", "b:view").To(serveOK)

	if got := statusWith(t, r, "a:view"); got != http.StatusForbidden {
		t.Fatalf("one of two: %d", got)
	}

	if got := statusWith(t, r, "a:view", "b:view"); got != http.StatusOK {
		t.Fatalf("both: %d", got)
	}
}

func TestPermissionRulesCannotBeMixedOrPublic(t *testing.T) {
	mixed := route.Get[route.None, string]("/x").Requires("a:view").RequiresAny("b:view").To(serveOK)
	if err := mixed.Mount(gin.New(), secured(authztest.AllowAll())); err == nil || !strings.Contains(err.Error(), "use one") {
		t.Fatalf("got %v", err)
	}

	public := route.Get[route.None, string]("/x").RequiresAll("a:view", "b:view").Public().To(serveOK)
	if err := public.Mount(gin.New(), secured(authztest.AllowAll())); err == nil || !strings.Contains(err.Error(), "Public") {
		t.Fatalf("got %v", err)
	}
}
