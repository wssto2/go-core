//nolint:testpackage,paralleltest,cyclop // uses the package's runnable helper; a real server, one flow
package gocore

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTelemetryIsTheHandleTheBuilderRecordsHTTPMetricsTo(t *testing.T) {
	app, port := runnable(t)
	tel := app.Telemetry()

	app.authenticate = []gin.HandlerFunc{passthrough}
	app.Routes(upRoute.To(pong))

	base := serving(t, app, port)

	if status := fetch(t, base+"/up").status; status != http.StatusOK {
		t.Fatalf("/up: %d", status)
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

	if !strings.Contains(fetch(t, base+"/metrics").body, `"/up"`) {
		t.Fatal("/metrics does not serve app.Telemetry()'s registry")
	}
}

func TestTelemetryIsOneHandle(t *testing.T) {
	t.Parallel()

	app := testApp(t, "local")

	first := app.Telemetry()
	if first == nil || first != app.Telemetry() {
		t.Fatal("Telemetry must return the same non-nil handle every time")
	}
}
