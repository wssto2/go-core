package product

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/gocoretest"
)

// newApp installs the feature on a test App signed in as user 1, who may do everything.
func newApp(t *testing.T) *gocore.App {
	t.Helper()

	app := gocoretest.New(t, gocoretest.SignedIn(authz.User(1, 0)), gocoretest.Authorizer(authztest.AllowAll()))

	catalogue := authz.NewCatalogue()
	DefinePermissions(catalogue)
	app.Permissions(catalogue)

	require.NoError(t, migrate(app.Database()))
	Install(app, t.TempDir())

	return app
}

func TestRoutes_CreateAndShow(t *testing.T) {
	app := newApp(t)

	rec := gocoretest.Do(t, app, http.MethodPost, "/v1/products", map[string]any{"name": "Widget", "sku": "WGT-1", "price": 12.34, "stock": 10})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	created := gocoretest.Decode[Product](t, rec)
	require.Equal(t, "WGT-1", created.SKU)

	rec = gocoretest.Do(t, app, http.MethodGet, fmt.Sprintf("/v1/products/%d", created.ID), nil)
	require.Equal(t, "WGT-1", gocoretest.Decode[Product](t, rec).SKU)
}

func TestRoutes_Create_ValidationError(t *testing.T) {
	app := newApp(t)

	// Missing required "name" and "sku" fields — should fail validation.
	rec := gocoretest.Do(t, app, http.MethodPost, "/v1/products", map[string]any{"price": 5.00})
	require.NotEqual(t, http.StatusCreated, rec.Code)
}

func TestRoutes_Delete_SoftDeletes(t *testing.T) {
	app := newApp(t)

	rec := gocoretest.Do(t, app, http.MethodPost, "/v1/products", map[string]any{"name": "ToDelete", "sku": "DEL-1", "price": 1.00})
	created := gocoretest.Decode[Product](t, rec)

	rec = gocoretest.Do(t, app, http.MethodDelete, fmt.Sprintf("/v1/products/%d", created.ID), nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	// It returns 404 after the soft delete.
	rec = gocoretest.Do(t, app, http.MethodGet, fmt.Sprintf("/v1/products/%d", created.ID), nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRoutes_WriteNeedsThePermission(t *testing.T) {
	app := gocoretest.New(t, gocoretest.SignedIn(authz.User(1, 0)), gocoretest.Authorizer(authztest.DenyAll()))

	catalogue := authz.NewCatalogue()
	DefinePermissions(catalogue)
	app.Permissions(catalogue)

	require.NoError(t, migrate(app.Database()))
	Install(app, t.TempDir())

	rec := gocoretest.Do(t, app, http.MethodPost, "/v1/products", map[string]any{"name": "Widget", "sku": "WGT-2", "price": 1})
	require.Equal(t, http.StatusForbidden, rec.Code)
}
