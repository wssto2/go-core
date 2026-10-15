//nolint:testpackage,paralleltest // uses the package's runnable helper; a real server
package gocore

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/bootstrap"
)

// stamp appends tag to the X-Order response header.
func stamp(tag string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Add("X-Order", tag)
		ctx.Next()
	}
}

// routeModule is an old-style module that mounts a route on the engine.
type routeModule struct{}

func (routeModule) Name() string { return "routes" }

func (routeModule) Register(c *bootstrap.Container) error {
	bootstrap.MustResolve[*gin.Engine](c).GET("/module", func(ctx *gin.Context) { ctx.String(http.StatusOK, "module") })

	return nil
}

func (routeModule) Boot(context.Context) error     { return nil }
func (routeModule) Shutdown(context.Context) error { return nil }

func TestUseRunsBeforeEveryRouteInTheOrderGivenWheneverItIsCalled(t *testing.T) {
	app, port := runnable(t)
	app.authenticate = []gin.HandlerFunc{stamp("auth")}

	// Modules come first in the container, and Use is called last: neither changes what runs before a route.
	app.Modules(routeModule{})
	app.Use(stamp("one"))
	app.Routes(upRoute.To(pong))
	app.Use(stamp("two"), stamp("three"))

	base := serving(t, app, port)

	for path, want := range map[string][]string{
		"/up":     {"one", "two", "three", "auth"},
		"/module": {"one", "two", "three"},
	} {
		if got := fetch(t, base+path).header.Values("X-Order"); !slices.Equal(got, want) {
			t.Errorf("%s: ran %v, want %v", path, got, want)
		}
	}
}

func TestUseOnHandlerRunsBeforeRoutesAddedBeforeIt(t *testing.T) {
	t.Parallel()

	app, _ := twoChains(t, nil)
	app.Routes(publicRoute("/early").To(serve("early")))
	app.Use(stamp("late"))

	handler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}

	if got := call(handler, "/early").Header().Values("X-Order"); !slices.Equal(got, []string{"late"}) {
		t.Fatalf("ran %v", got)
	}
}
