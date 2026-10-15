//nolint:testpackage // uses the package's runnable helper
package gocore

import (
	"context"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/observability"
)

// telemetryModule remembers the handle the container holds.
type telemetryModule struct{ got *observability.Telemetry }

func (m *telemetryModule) Name() string { return "telemetry" }

func (m *telemetryModule) Register(c *bootstrap.Container) error {
	m.got = bootstrap.MustResolve[*observability.Telemetry](c)

	return nil
}

func (m *telemetryModule) Boot(context.Context) error     { return nil }
func (m *telemetryModule) Shutdown(context.Context) error { return nil }

func TestTelemetryIsTheHandleTheBuilderRecordsHTTPMetricsTo(t *testing.T) {
	app, port := runnable(t)
	tel := app.Telemetry()
	seen := &telemetryModule{got: nil}

	app.authenticate = []gin.HandlerFunc{passthrough}
	app.Routes(upRoute.To(pong))
	app.Modules(seen)

	base := serving(t, app, port)

	if resp, _ := fetch(t, base+"/up"); resp.StatusCode != 200 {
		t.Fatalf("/up: %d", resp.StatusCode)
	}

	if seen.got != tel {
		t.Fatal("the container holds another handle than app.Telemetry()")
	}

	families, err := tel.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	var counted bool

	for _, family := range families {
		if !strings.HasSuffix(family.GetName(), "http_requests_total") {
			continue
		}

		for _, m := range family.GetMetric() {
			for _, label := range m.GetLabel() {
				counted = counted || label.GetValue() == "/up"
			}
		}
	}

	if !counted {
		t.Fatal("the request to /up is not in app.Telemetry()'s registry")
	}

	if _, body := fetch(t, base+"/metrics"); !strings.Contains(body, `"/up"`) {
		t.Fatal("/metrics does not serve app.Telemetry()'s registry")
	}
}

func TestTelemetryIsOneHandle(t *testing.T) {
	t.Parallel()

	app := testApp(t, "local")
	if app.Telemetry() == nil || app.Telemetry() != app.Telemetry() {
		t.Fatal("Telemetry must return the same non-nil handle every time")
	}
}
