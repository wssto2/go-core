//nolint:testpackage // uses the package's runnable and serving helpers
package gocore

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/tenancy"
)

type probeWorker struct{ seen chan bool }

func (probeWorker) Name() string { return "probe" }

func (w probeWorker) Run(ctx context.Context) error {
	w.seen <- tenancy.AllTenantsFromContext(ctx)

	<-ctx.Done()

	return nil
}

func TestACommandAndAWorkerActForNoTenantAndARequestDoesNot(t *testing.T) {
	t.Parallel()

	app, port := runnable(t)

	seen := make(chan bool, 1)

	app.Background(probeWorker{seen})

	request := make(chan bool, 1)

	app.Routes(route.Raw(http.MethodGet, "/tenants").Name("tenants").Public().To(func(ctx *gin.Context) {
		request <- tenancy.AllTenantsFromContext(ctx.Request.Context())

		ctx.Status(http.StatusNoContent)
	}))

	base := serving(t, app, port)

	select {
	case all := <-seen:
		if !all {
			t.Error("the worker's context is not marked all-tenants")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never ran")
	}

	resp, err := http.Get(base + "/tenants") //nolint:noctx // a test
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	if all := <-request; all {
		t.Error("a request's context must not be marked all-tenants")
	}
}

func TestACommandGetsAnAllTenantsContextThatOneTenantNarrows(t *testing.T) {
	t.Parallel()

	app, _ := runnable(t)

	var marked, narrowed bool

	var tenant int

	app.Command("probe", "", func(ctx context.Context, _ *Cmd) error {
		marked = tenancy.AllTenantsFromContext(ctx)
		tenant, narrowed = tenancy.TenantIDFromContext(tenancy.WithTenantID(ctx, 7))

		return nil
	})

	var out strings.Builder

	err := app.RunCommand(t.Context(), []string{"probe"}, &out)
	if err != nil {
		t.Fatal(err)
	}

	if !marked || !narrowed || tenant != 7 {
		t.Fatalf("marked=%t narrowed=%t tenant=%d", marked, narrowed, tenant)
	}
}
