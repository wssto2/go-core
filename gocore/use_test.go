//nolint:testpackage,paralleltest // uses the package's runnable helper; a real server
package gocore

import (
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
)

// stamp appends tag to the X-Order response header.
func stamp(tag string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Add("X-Order", tag)
		ctx.Next()
	}
}

func TestUseRunsBeforeEveryRouteInTheOrderGivenWheneverItIsCalled(t *testing.T) {
	app, port := runnable(t)
	app.authenticate = []gin.HandlerFunc{stamp("auth")}

	// Use is called around Routes: neither order changes what runs before a route.
	app.Use(stamp("one"))
	app.Routes(upRoute.To(pong))
	app.Use(stamp("two"), stamp("three"))

	base := serving(t, app, port)

	for path, want := range map[string][]string{
		"/up": {"one", "two", "three", "auth"},
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
