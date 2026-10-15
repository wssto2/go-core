//nolint:testpackage // uses the package's testApp
package gocore

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/route"
)

func TestCheckRefusesEveryRouteThatDeclaresNoAccess(t *testing.T) {
	app := testApp(t, "local")
	app.authenticate = []gin.HandlerFunc{passthrough}
	app.prefix = "/api"

	forgotten := route.Get[route.None, string]("/forgotten")
	app.Routes(forgotten.To(pong))
	app.Root(route.Get[route.None, string]("/rooted").To(pong))
	app.RoutesWith(route.Security{Authenticate: []gin.HandlerFunc{passthrough}}, route.Raw(http.MethodPost, "/hook").To(func(*gin.Context) {}))
	app.Routes(
		route.Get[route.None, string]("/a").Authenticated().To(pong),
		route.Get[route.None, string]("/b").PersonOnly().To(pong),
		route.Get[route.None, string]("/c").Public().To(pong),
	)

	err := app.Check()

	var se *StartupError
	if !errors.As(err, &se) || len(se.Problems) != 3 {
		t.Fatalf("want exactly the three forgotten routes, got %v", err)
	}

	for i, want := range []string{"GET /api/forgotten", "GET /rooted", "POST /api/hook"} {
		p := se.Problems[i]
		if !strings.Contains(p.What, "route "+want+" declares no access") {
			t.Errorf("problem %d: want it to name %q, got %q", i, want, p.What)
		}

		for _, choice := range []string{"Requires", "Public", "PersonOnly", "Authenticated"} {
			if !strings.Contains(p.What+p.Fix, choice) {
				t.Errorf("problem %d does not offer %s: %+v", i, choice, p)
			}
		}
	}
}

func TestServedAndContractsFollowTheInstalledRoutes(t *testing.T) {
	app := testApp(t, "local")
	app.prefix = "/api"

	a := route.Get[route.None, string]("/a").Public()
	b := route.Get[route.None, string]("/b").Authenticated()
	c := route.Get[route.None, string]("/c").Public()
	left := route.Get[route.None, string]("/left").Public()
	first, second := route.Group("first", a, b, left), route.Group("second", c)

	app.Routes(a.To(pong))
	app.Root(c.To(pong))
	app.Routes(b.To(pong))

	served := app.Served()
	if len(served) != 3 {
		t.Fatalf("want 3 served routes, got %v", served)
	}

	for i, want := range []string{"/api/a", "/c", "/api/b"} {
		if served[i].Path != want {
			t.Errorf("served[%d].Path = %q, want %q", i, served[i].Path, want)
		}
	}

	if served[0].Spec.Path != "/a" || !served[2].Spec.Authenticated {
		t.Errorf("the Spec is the declared one: %+v", served)
	}

	if got := app.Contracts(); len(got) != 2 || got[0] != first || got[1] != second {
		t.Errorf("want [first second], got %v", got)
	}
}
