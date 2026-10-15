//nolint:paralleltest,exhaustruct,varnamelen,noinlineerr,exhaustruct_v5 // tests
package route_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/route"
)

func TestDeclaresAccess(t *testing.T) {
	for name, tc := range map[string]struct {
		spec route.Spec
		want bool
	}{
		"nothing":       {route.Get[route.None, string]("/x").Spec(), false},
		"a raw nothing": {route.Raw("GET", "/x").Spec(), false},
		"requires":      {route.Get[route.None, string]("/x").Requires("a.b:view").Spec(), true},
		"requires any":  {route.Get[route.None, string]("/x").RequiresAny("a.b:view").Spec(), true},
		"requires all":  {route.Get[route.None, string]("/x").RequiresAll("a.b:view").Spec(), true},
		"public":        {route.Get[route.None, string]("/x").Public().Spec(), true},
		"person only":   {route.Get[route.None, string]("/x").PersonOnly().Spec(), true},
		"authenticated": {route.Get[route.None, string]("/x").Authenticated().Spec(), true},
		"raw":           {route.Raw("GET", "/x").Authenticated().Spec(), true},
	} {
		if got := tc.spec.DeclaresAccess(); got != tc.want {
			t.Errorf("%s: DeclaresAccess = %v, want %v", name, got, tc.want)
		}
	}
}

func TestAuthenticatedNeedsSignInAndNoPermission(t *testing.T) {
	routes := []route.Handled{
		route.Get[route.None, string]("/typed").Authenticated().To(
			func(_ context.Context, _ route.None) (string, error) { return "ok", nil }),
		route.Raw(http.MethodGet, "/raw").Authenticated().To(func(c *gin.Context) { c.String(http.StatusOK, "ok") }),
	}

	// A service account passes: Authenticated is any principal, PersonOnly is the narrower promise.
	e := newEngine()

	for _, r := range routes {
		if err := r.Mount(e, as(authz.ServiceAccount(5))); err != nil {
			t.Fatal(err)
		}
	}

	for _, path := range []string{"/typed", "/raw"} {
		if rec := do(e, http.MethodGet, path, ""); rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", path, rec.Code)
		}
	}

	// Without authentication configured, the route cannot be mounted at all.
	err := routes[0].Mount(newEngine(), route.Security{})
	if err == nil || !strings.Contains(err.Error(), "no authentication") {
		t.Errorf("want the missing authentication named, got %v", err)
	}
}

func TestAnAuthenticatedRouteCannotBePublic(t *testing.T) {
	handler := func(_ context.Context, _ route.None) (string, error) { return "", nil }

	err := route.Get[route.None, string]("/x").Public().Authenticated().To(handler).Mount(newEngine(), open())
	if err == nil || !strings.Contains(err.Error(), "Public but also Authenticated") {
		t.Fatalf("want the clash named, got %v", err)
	}

	err = route.Raw(http.MethodGet, "/y").Authenticated().Public().To(func(*gin.Context) {}).Mount(newEngine(), open())
	if err == nil || !strings.Contains(err.Error(), "Public but also Authenticated") {
		t.Fatalf("a raw route too, got %v", err)
	}
}

func TestContractsAreListedOncePerGroupInOrder(t *testing.T) {
	a := route.Get[route.None, string]("/a").Authenticated()
	b := route.Get[route.None, string]("/b").Authenticated()
	c := route.Get[route.None, string]("/c").Authenticated()
	first, second := route.Group("first", a, b), route.Group("second", b, c)
	alone := route.Get[route.None, string]("/alone").Authenticated()

	h := func(r route.Route[route.None, string]) route.Handled {
		return r.To(func(context.Context, route.None) (string, error) { return "", nil })
	}

	got := route.Contracts(h(c), h(a), h(b), h(alone))
	if len(got) != 2 || got[0] != second || got[1] != first {
		t.Fatalf("want [second first] (c's group first, once each, a route in no group adds none), got %v", got)
	}
}
