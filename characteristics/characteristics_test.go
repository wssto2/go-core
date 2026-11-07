//nolint:exhaustruct,exhaustruct_v5,varnamelen,paralleltest,lll,goconst // tests in the style of the package's others
package characteristics_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/characteristics"
	"github.com/wssto2/go-core/characteristics/core"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
)

const (
	webmaster = 1 // holds characteristics:view and :edit
	viewer    = 2 // holds characteristics:view only
	editor    = 3 // holds a permission of its own, nothing of the module
	prefix    = "/api"
	root      = prefix + "/v1/characteristics"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

// lazyAuthorizer is the application's authorizer before the engine exists: the engine needs the catalogue, which
// Install completes.
type lazyAuthorizer struct{ authz.Authorizer }

type uses map[int][]core.Use

func (u uses) Uses(_ context.Context, level, id int) ([]core.Use, error) {
	return u[level*1000+id], nil
}

type world struct {
	t       *testing.T
	handler http.Handler
	app     *gocore.App
	service *core.Service
}

// newWorld installs the module over default tables on SQLite, with a real authz engine and three roles.
func newWorld(t *testing.T, opts ...characteristics.Option) *world {
	t.Helper()

	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	gate := &lazyAuthorizer{}
	app := gocore.New(
		gocore.WithConfig(gocore.DefaultConfig()), gocore.WithRegistry(reg), gocore.WithAutoMigrate(t.Context()),
		gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithAuthentication(signIn), gocore.WithAuthorizer(gate),
		gocore.WithPrefix(prefix), gocore.WithClock(fixedClock{at: time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)}))

	catalogue := authz.NewCatalogue()
	catalogue.MustDefine("pages.page:edit")

	opts = append([]characteristics.Option{characteristics.WithMigrations(2), characteristics.WithoutUsage()}, opts...)
	service := characteristics.Install(app, catalogue, characteristics.Default(2), opts...)
	require.NotNil(t, service)

	w := authztest.NewWorld(t, catalogue,
		authz.Role{Key: "webmaster", Name: "Webmaster", Grants: characteristics.Grants()},
		authz.Role{Key: "viewer", Name: "Viewer", Grants: authz.Grants(authz.QualifierAll, characteristics.View)},
		authz.Role{Key: "editor", Name: "Editor", Grants: authz.Grants(authz.QualifierAll, "pages.page:edit")})
	gate.Authorizer = w.Engine

	for id, role := range map[int]string{webmaster: "webmaster", viewer: "viewer", editor: "editor"} {
		w.Bind(authz.Subject{Kind: authz.KindUser, ID: id}, role, authztest.Org())
	}

	handler, err := app.Handler()
	require.NoError(t, err)

	return &world{t: t, handler: handler, app: app, service: service}
}

func signIn(c *gin.Context) {
	id, err := strconv.Atoi(c.GetHeader("X-User"))
	if err != nil {
		_ = c.Error(apperr.Unauthorized("user not authenticated"))
		c.Abort()

		return
	}

	c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.User(id, 0)))
	c.Next()
}

type answer struct {
	Code int
	Body string
	Data json.RawMessage
}

func (w *world) as(who int, method, path string, body any) answer {
	w.t.Helper()

	raw, err := json.Marshal(body)
	require.NoError(w.t, err)

	req := httptest.NewRequestWithContext(w.t.Context(), method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")

	if who > 0 {
		req.Header.Set("X-User", strconv.Itoa(who))
	}

	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)

	return answer{Code: rec.Code, Body: rec.Body.String(), Data: envelope.Data}
}

func (a answer) into(t *testing.T, out any) {
	t.Helper()
	require.Equal(t, http.StatusOK, a.Code, a.Body)
	require.NoError(t, json.Unmarshal(a.Data, out))
}

func TestWebmasterReadsAndWritesTheTree(t *testing.T) {
	w := newWorld(t)

	var top characteristics.ItemResponse

	w.as(webmaster, http.MethodPost, root+"/items/1", map[string]any{
		"name": "Novosti", "active": true, "translations": map[string]string{"hr": "Novosti", "en": "News"}, "meta": map[string]any{"icon": "n"},
	}).into(t, &top)
	require.Equal(t, characteristics.ItemResponse{
		Level: 1, ID: 1, Name: "Novosti", Translations: map[string]string{"hr": "Novosti", "en": "News"}, Order: 1, Active: true,
		Meta: map[string]any{"icon": "n"}, Values: map[string]any{},
	}, top)

	var child characteristics.ItemResponse

	w.as(webmaster, http.MethodPost, root+"/items/2", map[string]any{"name": "Kategorije", "parent_id": top.ID}).into(t, &child)
	require.Equal(t, top.ID, child.ParentID)
	require.False(t, child.Active)

	w.as(webmaster, http.MethodPut, root+"/items/2/"+strconv.Itoa(child.ID), map[string]any{"name": "Vrste", "translations": map[string]string{"en": "Kinds"}}).into(t, &child)
	require.Equal(t, "Vrste", child.Name)
	require.Equal(t, map[string]string{"en": "Kinds"}, child.Translations)

	w.as(webmaster, http.MethodPut, root+"/items/2/"+strconv.Itoa(child.ID)+"/active", map[string]any{"active": true}).into(t, &child)
	require.True(t, child.Active)

	var tree characteristics.TreeResponse

	w.as(viewer, http.MethodGet, root, nil).into(t, &tree)
	require.Len(t, tree.Items, 2)
	require.Equal(t, []int{1, 2}, []int{tree.Items[0].Level, tree.Items[1].Level}, "a parent comes before its children")

	var description characteristics.DescriptionResponse

	w.as(viewer, http.MethodGet, root+"/description", nil).into(t, &description)
	require.Equal(t, []string{"hr", "en"}, description.Languages)
	require.Len(t, description.Levels, 2)
	require.True(t, description.Levels[1].HasOrder)

	var usage characteristics.UsageResponse

	w.as(viewer, http.MethodGet, root+"/items/1/1/usage", nil).into(t, &usage)
	require.False(t, usage.Known, "no usage port: usage unknown")
	require.Equal(t, 1, usage.Children)

	// A bad write is a 400 naming the problem; an unknown item is a 404.
	require.Equal(t, http.StatusBadRequest, w.as(webmaster, http.MethodPost, root+"/items/2", map[string]any{"name": "Orphan", "parent_id": 99}).Code)
	require.Equal(t, http.StatusNotFound, w.as(webmaster, http.MethodGet, root+"/items/1/99", nil).Code)
	require.Equal(t, http.StatusNotFound, w.as(webmaster, http.MethodGet, root+"/items/7/1", nil).Code)

	// The audit columns were written with the signed-in person and the clock.
	var raw struct {
		CreatedBy   int
		ActivatedBy int
		UpdatedAt   string
	}

	require.NoError(t, w.app.Database().Raw("SELECT created_by, activated_by, updated_at FROM characteristics_level1 WHERE id = 1").Scan(&raw).Error)
	require.Equal(t, webmaster, raw.CreatedBy)
	require.Equal(t, webmaster, raw.ActivatedBy)
	require.Contains(t, raw.UpdatedAt, "2026-03-04")
}

func TestPermissions(t *testing.T) {
	w := newWorld(t)
	w.as(webmaster, http.MethodPost, root+"/items/1", map[string]any{"name": "Marke", "active": true})

	writes := []struct{ method, path string }{
		{http.MethodPost, root + "/items/1"}, {http.MethodPut, root + "/items/1/1"}, {http.MethodPut, root + "/items/1/1/active"},
		{http.MethodPut, root + "/items/1/1/position"}, {http.MethodDelete, root + "/items/1/1"},
	}
	for _, write := range writes {
		body := map[string]any{"name": "x", "position": 1}
		require.Equal(t, http.StatusForbidden, w.as(viewer, write.method, write.path, body).Code, "viewer "+write.method+" "+write.path)
		require.Equal(t, http.StatusForbidden, w.as(editor, write.method, write.path, body).Code, "editor "+write.method+" "+write.path)
		require.Equal(t, http.StatusUnauthorized, w.as(0, write.method, write.path, body).Code, "nobody "+write.method+" "+write.path)
	}

	for _, read := range []string{root, root + "/description", root + "/items/1/1", root + "/items/1/1/usage", root + "/items/1/1/branch"} {
		require.Equal(t, http.StatusOK, w.as(viewer, http.MethodGet, read, nil).Code, "viewer "+read)
		require.Equal(t, http.StatusForbidden, w.as(editor, http.MethodGet, read, nil).Code, "editor "+read)
	}

	// Options need a signed-in caller and no permission: editors pick characteristics in their forms.
	var options []characteristics.OptionResponse

	w.as(editor, http.MethodGet, root+"/options?level=1&lang=en", nil).into(t, &options)
	require.Equal(t, []characteristics.OptionResponse{{Value: "1", Label: "Marke"}}, options)
	require.Equal(t, http.StatusUnauthorized, w.as(0, http.MethodGet, root+"/options?level=1", nil).Code)
}

func TestDeleteRefusedWhileUsedAndForced(t *testing.T) {
	w := newWorld(t, characteristics.WithUsage(uses{1001: {{What: "pages", Count: 2}, {What: "hosts", Count: 1}}}))
	w.as(webmaster, http.MethodPost, root+"/items/1", map[string]any{"name": "Marke"})

	refused := w.as(webmaster, http.MethodDelete, root+"/items/1/1", nil)
	require.Equal(t, http.StatusConflict, refused.Code, refused.Body)
	require.Contains(t, refused.Body, "characteristics.in_use")
	require.Contains(t, refused.Body, `"pages"`)

	var usage characteristics.UsageResponse

	w.as(viewer, http.MethodGet, root+"/items/1/1/usage", nil).into(t, &usage)
	require.True(t, usage.Known)
	require.Equal(t, []characteristics.UseResponse{{What: "pages", Count: 2}, {What: "hosts", Count: 1}}, usage.Uses)

	var switched characteristics.ItemResponse

	w.as(webmaster, http.MethodPut, root+"/items/1/1/active", map[string]any{"active": false}).into(t, &switched)
	require.False(t, switched.Active, "deactivating is always possible")

	var deleted characteristics.DeleteResponse

	w.as(webmaster, http.MethodDelete, root+"/items/1/1?force=true", nil).into(t, &deleted)
	require.Equal(t, characteristics.DeleteResponse{Deleted: 1, UsageKnown: true}, deleted)
}

func TestDeleteWithoutAPortSaysUsageUnknown(t *testing.T) {
	w := newWorld(t)
	w.as(webmaster, http.MethodPost, root+"/items/1", map[string]any{"name": "Marke"})

	var deleted characteristics.DeleteResponse

	w.as(webmaster, http.MethodDelete, root+"/items/1/1", nil).into(t, &deleted)
	require.Equal(t, characteristics.DeleteResponse{Deleted: 1, UsageKnown: false}, deleted)
}

func TestCreateMoveAndExtraColumnsOverHTTP(t *testing.T) {
	w := newWorld(t)

	for _, name := range []string{"A", "B", "C"} {
		w.as(webmaster, http.MethodPost, root+"/items/1", map[string]any{"name": name})
	}

	require.Equal(t, http.StatusNoContent, w.as(webmaster, http.MethodPut, root+"/items/1/3/position", map[string]any{"position": 1}).Code)

	var tree characteristics.TreeResponse

	w.as(viewer, http.MethodGet, root, nil).into(t, &tree)
	require.Equal(t, []string{"C", "A", "B"}, []string{tree.Items[0].Name, tree.Items[1].Name, tree.Items[2].Name})

	// A value for a column the description does not name is refused, not stored.
	rejected := w.as(webmaster, http.MethodPut, root+"/items/1/1", map[string]any{"name": "A", "values": map[string]any{"legacy": "x"}})
	require.Equal(t, http.StatusBadRequest, rejected.Code, rejected.Body)
}

func TestInstallStopsOnAProblem(t *testing.T) {
	check := func(catalogue *authz.Catalogue, tree core.Tree, opts ...characteristics.Option) error {
		reg, cleanup := database.NewTestRegistry("local")
		defer func() { _ = cleanup() }()

		app := gocore.New(gocore.WithConfig(gocore.DefaultConfig()), gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)),
			gocore.WithAuthentication(signIn), gocore.WithAuthorizer(authztest.AllowAll()))

		service := characteristics.Install(app, catalogue, tree, opts...)
		require.Nil(t, service)

		return app.Check()
	}

	err := check(authz.NewCatalogue(), characteristics.Default(1))
	require.ErrorContains(t, err, "WithoutUsage", "neither WithUsage nor WithoutUsage")

	err = check(nil, characteristics.Default(1), characteristics.WithoutUsage())
	require.ErrorContains(t, err, "permission catalogue")

	err = check(authz.NewCatalogue(), characteristics.Default(1), characteristics.WithoutUsage(), characteristics.WithMigrations(4))
	require.ErrorContains(t, err, "1 to 3 levels")

	bad := characteristics.Default(2)
	bad.Levels[1].Parent = ""
	bad.Levels[1].Fields = []core.Field{{Column: "x y", Type: "money"}}

	err = check(authz.NewCatalogue(), bad, characteristics.WithoutUsage())
	require.ErrorContains(t, err, "characteristics_level2")
	require.ErrorContains(t, err, "the parent column is missing")
	require.ErrorContains(t, err, `type "money"`)
}

func TestTheModuleDefinesAndGrantsItsPermissions(t *testing.T) {
	w := newWorld(t)
	catalogue := w.app.Catalogues()[0]

	view, ok := catalogue.Lookup(characteristics.View)
	require.True(t, ok)
	require.Equal(t, "perm.characteristics.view", view.LabelKey)

	edit, ok := catalogue.Lookup(characteristics.Edit)
	require.True(t, ok)
	require.Equal(t, []string{characteristics.View}, edit.Requires)

	for key := range map[string]bool{view.LabelKey: true, edit.LabelKey: true} {
		require.NotEmpty(t, characteristics.Labels[key].HR)
		require.NotEmpty(t, characteristics.Labels[key].EN)
	}

	require.NoError(t, authz.Role{Key: "webmaster", Name: "Webmaster", Grants: characteristics.Grants()}.Validate(catalogue))
}
