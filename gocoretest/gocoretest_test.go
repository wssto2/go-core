package gocoretest_test

import (
	"net/http"
	"testing"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/route"
)

var Secret = route.Get[ShowInput, Ticket]("/secrets/:id").Requires("ops.secret:view")

func TestPermissionRoutesNeedACatalogueAndAnAuthorizer(t *testing.T) {
	catalogue := authz.NewCatalogue()
	catalogue.MustDefine("ops.secret:view")

	t.Run("denied", func(t *testing.T) {
		app := gocoretest.New(t, gocoretest.Authorizer(authztest.DenyAll()))
		app.Permissions(catalogue)
		app.Routes(Secret.To(Service{app}.Show))

		if rec := gocoretest.Do(t, app, http.MethodGet, "/secrets/1", nil); rec.Code != http.StatusForbidden {
			t.Fatalf("status %d", rec.Code)
		}
	})

	t.Run("allowed", func(t *testing.T) {
		app := gocoretest.New(t, gocoretest.Authorizer(authztest.AllowAll()))
		app.Permissions(catalogue)
		app.Routes(Secret.To(Service{app}.Show))

		got := gocoretest.Decode[Ticket](t, gocoretest.Do(t, app, http.MethodGet, "/secrets/4", nil))
		if got.ID != 4 {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestNamedDatabasesAreRegistered(t *testing.T) {
	app := gocoretest.New(t, gocoretest.Databases("local", "shared"))

	if err := app.Database("shared").Exec("select 1").Error; err != nil {
		t.Fatal(err)
	}

	if err := app.Check(); err != nil {
		t.Fatal(err)
	}
}
