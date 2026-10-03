package gocoretest_test

import (
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/route"
)

var Open = route.Get[ShowInput, Ticket]("/open/:id").Public()

var Secret = route.Get[ShowInput, Ticket]("/secrets/:id").Requires("ops.secret:view")

func TestPermissionRoutesNeedACatalogueAndAnAuthorizer(t *testing.T) {
	catalogue := authz.NewCatalogue()
	catalogue.MustDefine("ops.secret:view")

	t.Run("denied", func(t *testing.T) {
		app := gocoretest.New(t, gocoretest.SignedIn(authz.User(1, 0)), gocoretest.Authorizer(authztest.DenyAll()))
		app.Permissions(catalogue)
		app.Routes(Secret.To(Service{app}.Show))

		if rec := gocoretest.Do(t, app, http.MethodGet, "/secrets/1", nil); rec.Code != http.StatusForbidden {
			t.Fatalf("status %d", rec.Code)
		}
	})

	t.Run("allowed", func(t *testing.T) {
		app := gocoretest.New(t, gocoretest.SignedIn(authz.User(1, 0)), gocoretest.Authorizer(authztest.AllowAll()))
		app.Permissions(catalogue)
		app.Routes(Secret.To(Service{app}.Show))

		got := gocoretest.Decode[Ticket](t, gocoretest.Do(t, app, http.MethodGet, "/secrets/4", nil))
		if got.ID != 4 {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestAnonymousRequestsAreRejectedExceptForPublicRoutes(t *testing.T) {
	app := gocoretest.New(t)
	app.Routes(Show.To(Service{app}.Show), Open.To(Service{app}.Show))

	if rec := gocoretest.Do(t, app, http.MethodGet, "/tickets/1", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("private route: status %d", rec.Code)
	}

	if rec := gocoretest.Do(t, app, http.MethodGet, "/open/1", nil); rec.Code != http.StatusOK {
		t.Fatalf("public route: status %d", rec.Code)
	}
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

func TestNewMigratesAsFeaturesInstall(t *testing.T) {
	app := gocoretest.New(t)
	app.Migrations(fstest.MapFS{"20261015000000_things.sql": {Data: []byte("-- +goose Up\nCREATE TABLE things (id INTEGER);")}})

	if err := app.Database().Exec("INSERT INTO things (id) VALUES (1)").Error; err != nil {
		t.Fatal(err)
	}
}
