package gocore_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/route"
)

func signedInChain(c *gin.Context) { c.Next() }

func answer(context.Context, route.None) (string, error) { return "ok", nil }

func newAccessApp() *gocore.App {
	return gocore.New(bootstrap.DefaultConfig(), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithAuthentication(signedInChain), gocore.WithPrefix("/api"))
}

func TestCheckRefusesEveryRouteThatDeclaresNoAccess(t *testing.T) {
	t.Parallel()

	app := newAccessApp()

	forgotten := route.Get[route.None, string]("/forgotten")
	app.Routes(forgotten.To(answer))
	app.Root(route.Get[route.None, string]("/rooted").To(answer))

	hooks := route.Security{Authenticate: []gin.HandlerFunc{signedInChain}, Authorizer: nil}
	app.RoutesWith(hooks, route.Raw(http.MethodPost, "/hook").To(func(*gin.Context) {}))
	app.Routes(
		route.Get[route.None, string]("/a").Authenticated().To(answer),
		route.Get[route.None, string]("/b").PersonOnly().To(answer),
		route.Get[route.None, string]("/c").Public().To(answer),
	)

	err := app.Check()

	var startup *gocore.StartupError
	if !errors.As(err, &startup) || len(startup.Problems) != 3 {
		t.Fatalf("want exactly the three forgotten routes, got %v", err)
	}

	for idx, want := range []string{"GET /api/forgotten", "GET /rooted", "POST /api/hook"} {
		problem := startup.Problems[idx]
		if !strings.Contains(problem.What, "route "+want+" declares no access") {
			t.Errorf("problem %d: want it to name %q, got %q", idx, want, problem.What)
		}

		for _, choice := range []string{"Requires", "Public", "PersonOnly", "Authenticated"} {
			if !strings.Contains(problem.What+problem.Fix, choice) {
				t.Errorf("problem %d does not offer %s: %+v", idx, choice, problem)
			}
		}
	}
}

func TestServedAndContractsFollowTheInstalledRoutes(t *testing.T) {
	t.Parallel()

	app := newAccessApp()

	first := route.Get[route.None, string]("/a").Public()
	second := route.Get[route.None, string]("/b").Authenticated()
	third := route.Get[route.None, string]("/c").Public()
	left := route.Get[route.None, string]("/left").Public()
	one, two := route.Group("one", first, second, left), route.Group("two", third)

	app.Routes(first.To(answer))
	app.Root(third.To(answer))
	app.Routes(second.To(answer))

	served := app.Served()
	if len(served) != 3 {
		t.Fatalf("want 3 served routes, got %v", served)
	}

	for idx, want := range []string{"/api/a", "/c", "/api/b"} {
		if served[idx].Path != want {
			t.Errorf("served[%d].Path = %q, want %q", idx, served[idx].Path, want)
		}
	}

	if served[0].Spec.Path != "/a" || !served[2].Spec.Authenticated {
		t.Errorf("the Spec is the declared one: %+v", served)
	}

	if got := app.Contracts(); len(got) != 2 || got[0] != one || got[1] != two {
		t.Errorf("want [one two], got %v", got)
	}
}
