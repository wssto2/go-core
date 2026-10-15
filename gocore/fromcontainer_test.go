//nolint:testpackage // uses the package's runnable and hookModule
package gocore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/route"
)

type greeter struct{ word string }

// bindingModule is an old module: it builds its service into the container.
type bindingModule struct{ hookModule }

func (bindingModule) Register(c *bootstrap.Container) error {
	bootstrap.Bind(c, &greeter{word: "hello"})

	return nil
}

func TestFromContainerIsKeptFromTheOldModulesBeforeRoutesServe(t *testing.T) {
	t.Parallel()

	app, port := runnable(t)
	app.authenticate = []gin.HandlerFunc{passthrough}

	greeters := FromContainer[*greeter](app)

	err := app.Check()
	if err != nil {
		t.Fatalf("Check must not ask for what the container provides at Run: %v", err)
	}

	app.Routes(route.Get[route.None, string]("/greet").Authenticated().To(func(context.Context, route.None) (string, error) {
		g, err := greeters.Get()

		return g.word, err
	}))
	app.Modules(&bindingModule{hookModule{name: "old", bootErr: nil, onStop: func() {}}})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- app.RunContext(ctx) }()

	var body string

	waitFor(t, "the server", func() bool {
		select {
		case err := <-done:
			t.Fatalf("Run ended early: %v", err)
		default:
		}

		resp, err := get(fmt.Sprintf("http://127.0.0.1:%d/greet", port))
		if err != nil {
			return false
		}

		defer func() { _ = resp.Body.Close() }()

		b, _ := io.ReadAll(resp.Body)
		body = string(b)

		return resp.StatusCode == http.StatusOK
	})

	cancel()

	err = <-done
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(body, "hello") {
		t.Fatalf("the handler did not get the container's value: %s", body)
	}
}

func TestFromContainerRefusesToStartNamingTheTypeNobodyBound(t *testing.T) {
	t.Parallel()

	app, _ := runnable(t)
	FromContainer[*greeter](app)

	err := app.RunContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "FromContainer[*gocore.greeter]") {
		t.Fatalf("want an error naming the type, got %v", err)
	}
}

func TestFromContainerReportsAStrictContainersPanicAsTheSameError(t *testing.T) {
	t.Parallel()

	c := bootstrap.NewContainer()
	c.EnableStrictMode()

	d := &Deferred[*greeter]{value: atomic.Pointer[*greeter]{}, fromContainer: true}

	err := d.fill(c)
	if err == nil || !strings.Contains(err.Error(), "FromContainer[*gocore.greeter]") {
		t.Fatalf("got %v", err)
	}
}

func TestFromContainerGetBeforeItIsKeptNamesTheType(t *testing.T) {
	t.Parallel()

	d := FromContainer[*greeter](testApp(t, "local"))

	_, err := d.Get()
	if err == nil || !strings.Contains(err.Error(), "*gocore.greeter") {
		t.Fatalf("got %v", err)
	}
}
