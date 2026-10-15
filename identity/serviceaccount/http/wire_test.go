package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access/accesshttp"
	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	authzgorm "github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/identity/serviceaccount"
	sahttp "github.com/wssto2/go-core/identity/serviceaccount/http"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/route"
)

// The wire of the service account routes, pinned: every request of arv-next's own wire pin
// (testdata/arv_wire_serviceaccounts.json, which pinned arv-next's routes with the same calls and the
// same canned answers) is made again against these routes, and answers are recorded in
// testdata/wire_serviceaccounts.json. TestWireMatchesARV holds the two together: what differs is listed,
// with its reason, in accepted below. Regenerate on purpose: WIRE_PIN_UPDATE=1 go test ./identity/serviceaccount/http.

var at = time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

func asJSON(v any) string {
	out, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(out)
}

func refuse(id int) error {
	switch id {
	case 404:
		return apperr.NotFound("service account not found")
	case 409:
		return apperr.ConflictErr(errors.New("already deactivated"))
	}

	return nil
}

func view(id int, note string) serviceaccount.AccountView {
	used := at

	return serviceaccount.AccountView{
		ID: id, Name: "Robot " + strconv.Itoa(id), Description: note, Tenant: &serviceaccount.TenantRef{ID: 5, Name: "Dealer Five"},
		Owner: serviceaccount.Person{ID: 2, Name: "Ana"}, CreatedBy: serviceaccount.Person{ID: 1, Name: "Web"}, CreatedAt: at,
		ActiveKeys: 2, LastUsedAt: &used,
	}
}

// fakeAccounts says what it was asked: the ids and the inputs.
type fakeAccounts struct{}

func (fakeAccounts) List(context.Context) ([]serviceaccount.AccountView, error) {
	return []serviceaccount.AccountView{view(1, "one"), view(2, "two")}, nil
}

func (fakeAccounts) Get(_ context.Context, id int) (serviceaccount.AccountView, error) {
	return view(id, "get"), refuse(id)
}

func (fakeAccounts) Create(_ context.Context, in serviceaccount.SaveInput) (serviceaccount.AccountView, error) {
	return view(31, asJSON(in)), nil
}

func (fakeAccounts) Update(_ context.Context, id int, in serviceaccount.SaveInput) (serviceaccount.AccountView, error) {
	return view(id, asJSON(in)), refuse(id)
}

func (fakeAccounts) Deactivate(_ context.Context, id int) (serviceaccount.AccountView, error) {
	return view(id, "deactivated"), refuse(id)
}

func (fakeAccounts) Keys(_ context.Context, id int) ([]serviceaccount.KeyView, error) {
	return []serviceaccount.KeyView{{ID: 3, Name: "ci", Prefix: "arv_ab", CreatedBy: serviceaccount.Person{ID: 1, Name: "Web"}, CreatedAt: at}}, refuse(id)
}

func (fakeAccounts) CreateKey(_ context.Context, id int, in serviceaccount.KeyInput) (serviceaccount.CreatedKey, error) {
	return serviceaccount.CreatedKey{
		Key:    serviceaccount.KeyView{ID: 4, Name: asJSON(in), Prefix: "arv_cd", CreatedBy: serviceaccount.Person{ID: 1, Name: "Web"}, CreatedAt: at},
		Secret: "arv_secret",
	}, refuse(id)
}

func (fakeAccounts) RevokeKey(_ context.Context, id, keyID int) error {
	if keyID == 404 {
		return apperr.NotFound("key not found")
	}

	return refuse(id)
}

// held maps a user id to the permissions the user holds: the authorizer of the pin.
type held map[int][]string

func (h held) Require(ctx context.Context, permission string) error {
	p, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return authz.ErrNoPrincipal
	}

	if slices.Contains(h[p.ID], permission) {
		return nil
	}

	return authz.Deny(permission)
}

func (h held) RequireOn(ctx context.Context, permission string, _ authz.Resource) error {
	return h.Require(ctx, permission)
}

func (held) Access(context.Context, string) (authz.AccessSet, error) { return authz.AccessSet{}, nil }

type user struct{ id int }

func (u user) GetID() int { return u.id }

// provider verifies the tokens "u<id>".
type provider struct{}

func (provider) Verify(_ context.Context, token string) (auth.Identifiable, error) {
	id, err := strconv.Atoi(strings.TrimPrefix(token, "u"))
	if err != nil {
		return nil, auth.ErrUnauthorized
	}

	return user{id: id}, nil
}

// robots is the directory of the access service: service accounts 1 to 9 exist.
type robots struct{}

func (robots) SubjectNames(_ context.Context, subjects []authz.Subject) (map[authz.Subject]string, error) {
	out := map[authz.Subject]string{}

	for _, s := range subjects {
		if s.Kind == authz.KindServiceAccount && s.ID >= 1 && s.ID <= 9 {
			out[s] = "Robot " + strconv.Itoa(s.ID)
		}
	}

	return out, nil
}

// newBindings is the access service the access routes of service accounts run over: a real engine and a store.
func newBindings(t *testing.T) *admin.Bindings {
	t.Helper()

	cat := authz.NewCatalogue()
	for _, id := range admin.PermissionIDs() {
		cat.MustDefine(id)
	}

	reg, cleanup := database.NewTestRegistry("local")
	t.Cleanup(func() { _ = cleanup() })

	db := reg.MustGet("local")
	require.NoError(t, authzgorm.Migrate(db))

	store := authzgorm.New(db)
	places := authztest.NewPlaces().AddDealer(5)

	engine, err := authz.NewEngine(authz.Config{Catalogue: cat, Hierarchy: authztest.Hierarchy(), Store: store, Resolver: places})
	require.NoError(t, err)

	_, bindings, err := admin.New(admin.Config{
		Engine: engine, Store: store, Scopes: scopes{places}, Subjects: robots{}, Transactor: database.NewTransactor(db),
	})
	require.NoError(t, err)

	return bindings
}

type scopes struct{ *authztest.Places }

func (scopes) Names(context.Context, []authz.Scope) (map[authz.Scope]string, error) { return nil, nil }
func (scopes) Options(context.Context) ([]admin.ScopeOption, error)                 { return nil, nil }

func router(t *testing.T, h held) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	translator, err := i18n.New(i18n.Config{I18nDir: t.TempDir()})
	require.NoError(t, err)

	r := gin.New()
	r.Use(middlewares.RequestID(), middlewares.ErrorHandler(slog.New(slog.DiscardHandler), translator, false))
	r.Use(func(c *gin.Context) {
		if token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer u"); ok {
			if id, err := strconv.Atoi(token); err == nil {
				c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.User(id, 0)))
			}
		}

		c.Next()
	})

	security := route.Security{Authenticate: []gin.HandlerFunc{auth.Authenticated(provider{})}, Authorizer: h}
	group := r.Group("/api")

	for _, handled := range append(sahttp.Routes(fakeAccounts{}), accesshttp.ServiceAccountRoutes(newBindings(t))...) {
		require.NoError(t, handled.Mount(group, security))
	}

	return r
}

// exchange is one request and its response, as the golden files hold them.
type exchange struct {
	Request struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		As      string            `json:"as,omitempty"`
		Headers map[string]string `json:"headers,omitempty"`
		Body    any               `json:"body,omitempty"`
	} `json:"request"`
	Response struct {
		Status      int    `json:"status"`
		ContentType string `json:"content_type,omitempty"`
		Body        any    `json:"body,omitempty"`
	} `json:"response"`
}

func parse(raw string) any {
	if raw == "" {
		return nil
	}

	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}

	return v
}

// pin records the exchanges under the name of the case.
type pin struct {
	t        *testing.T
	router   *gin.Engine
	recorded map[string]exchange
}

func (p *pin) call(name string, user int, method, path, body string) {
	p.t.Helper()

	req := httptest.NewRequestWithContext(p.t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	var x exchange

	x.Request.As = "nobody"
	if user > 0 {
		x.Request.As = "u" + strconv.Itoa(user)
		x.Request.Headers = map[string]string{"Authorization": "Bearer u" + strconv.Itoa(user)}
		req.Header.Set("Authorization", "Bearer u"+strconv.Itoa(user))
	}

	rec := httptest.NewRecorder()
	p.router.ServeHTTP(rec, req)

	_, taken := p.recorded[name]
	require.False(p.t, taken, "case %q is recorded twice", name)

	x.Request.Method, x.Request.Path, x.Request.Body = method, path, parse(body)
	x.Response.Status, x.Response.ContentType, x.Response.Body = rec.Code, rec.Header().Get("Content-Type"), parse(rec.Body.String())
	p.recorded[name] = x
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	require.NoError(t, enc.Encode(v))

	return buf.Bytes()
}

const (
	pinFile = "wire_serviceaccounts.json"
	arvFile = "arv_wire_serviceaccounts.json"
)

// record makes every call of arv-next's pin.
func record(t *testing.T) map[string]exchange {
	t.Helper()

	r := router(t, held{
		1: {serviceaccount.View, serviceaccount.Manage, serviceaccount.Revoke},
		2: {serviceaccount.View},
		4: {serviceaccount.Manage},
		5: {serviceaccount.Revoke},
	})
	p := &pin{t: t, router: r, recorded: map[string]exchange{}}

	const (
		base = "/api/v1/iam/service-accounts"
		save = `{"name":"  Robot  ","description":" for ci ","tenant_id":5,"owner_id":2}`
		bind = `{"role_ref":"sales","level":"location","scope_id":9}`
	)

	type req struct{ method, path, body string }

	for _, rt := range []struct {
		name string
		user int
		r    req
	}{
		{"list", 2, req{nethttp.MethodGet, base, ""}},
		{"record", 2, req{nethttp.MethodGet, base + "/5", ""}},
		{"create", 4, req{nethttp.MethodPost, base, save}},
		{"update", 4, req{nethttp.MethodPut, base + "/5", save}},
		{"deactivate", 4, req{nethttp.MethodPost, base + "/5/deactivate", ""}},
		{"keys", 2, req{nethttp.MethodGet, base + "/5/keys", ""}},
		{"create a key", 4, req{nethttp.MethodPost, base + "/5/keys", `{"name":" ci ","expires_at":"2027-01-01T00:00:00Z"}`}},
		{"revoke a key", 5, req{nethttp.MethodPost, base + "/5/keys/3/revoke", ""}},
		{"access", 2, req{nethttp.MethodGet, base + "/5/access", ""}},
		{"scopes", 4, req{nethttp.MethodGet, base + "/5/scopes", ""}},
		{"bind", 4, req{nethttp.MethodPost, base + "/5/bindings", bind}},
		{"unbind", 4, req{nethttp.MethodDelete, base + "/5/bindings/21", ""}},
		{"bindable roles", 4, req{nethttp.MethodGet, base + "/bindable-roles?level=location&scope_id=9", ""}},
	} {
		for _, u := range []int{rt.user, 1, 3, 0} {
			p.call(rt.name+" as user "+strconv.Itoa(u), u, rt.r.method, rt.r.path, rt.r.body)
		}
	}

	// the service's refusals
	p.call("record, unknown", 2, nethttp.MethodGet, base+"/404", "")
	p.call("update, unknown", 4, nethttp.MethodPut, base+"/404", save)
	p.call("deactivate, done already", 4, nethttp.MethodPost, base+"/409/deactivate", "")
	p.call("keys, unknown", 2, nethttp.MethodGet, base+"/404/keys", "")
	p.call("create a key, unknown account", 4, nethttp.MethodPost, base+"/404/keys", `{"name":"ci"}`)
	p.call("revoke a key, unknown key", 5, nethttp.MethodPost, base+"/5/keys/404/revoke", "")
	p.call("revoke a key, unknown account", 5, nethttp.MethodPost, base+"/404/keys/3/revoke", "")
	p.call("access, unknown", 2, nethttp.MethodGet, base+"/404/access", "")
	p.call("access, not manageable", 2, nethttp.MethodGet, base+"/8/access", "")
	p.call("scopes, unknown", 4, nethttp.MethodGet, base+"/404/scopes", "")
	p.call("bind, unknown account", 4, nethttp.MethodPost, base+"/404/bindings", bind)
	p.call("bind at the organization", 4, nethttp.MethodPost, base+"/5/bindings", `{"role_ref":"sales","level":"organization"}`)
	p.call("unbind, unknown binding", 4, nethttp.MethodDelete, base+"/5/bindings/404", "")
	p.call("unbind, unknown account", 4, nethttp.MethodDelete, base+"/404/bindings/21", "")

	// the bindable roles' query
	for _, q := range []struct{ name, query string }{
		{"the organization", "?level=organization"}, {"no level", ""}, {"a text scope", "?level=dealer&scope_id=abc"},
		{"an empty scope", "?level=dealer&scope_id="}, {"a negative scope", "?level=dealer&scope_id=-2"},
		{"a repeated level", "?level=dealer&level=location&scope_id=1"}, {"a signed scope", "?level=dealer&scope_id=%2B7"},
		{"a big scope", "?level=dealer&scope_id=99999999999999999999"}, {"a fractional scope", "?level=dealer&scope_id=1.5"},
	} {
		p.call("bindable roles, "+q.name, 4, nethttp.MethodGet, base+"/bindable-roles"+q.query, "")
	}

	p.call("bindable roles, without permission", 2, nethttp.MethodGet, base+"/bindable-roles?level=dealer&scope_id=1", "")

	// bodies: bad, empty, wrong-typed, over the limits
	// the names of the cases are arv-next's, whose pin they are looked up in
	for _, b := range []struct{ name, body string }{
		{"bad json", `{"name":`}, {"empty object", `{}`}, {"no body", ``},
		{"wrong type", `{"name":5,"description":[],"tenant_id":"x","owner_id":"y"}`}, {"array", `[]`}, {"null", `null`},
		{"unknown field", `{"name":"a","owner_id":2,"x":1}`},
		{"a long name", `{"name":"` + strings.Repeat("a", 101) + `","owner_id":2}`},
		{"a long description", `{"name":"a","description":"` + strings.Repeat("a", 501) + `","owner_id":2}`},
		{"no responsible person", `{"name":"a","owner_id":0}`},
		{"a null dealer", `{"name":"a","tenant_id":null,"owner_id":2}`},
		{"a zero dealer", `{"name":"a","tenant_id":0,"owner_id":2}`},
	} {
		p.call("create with "+b.name, 4, nethttp.MethodPost, base, b.body)
		p.call("create with "+b.name+" without permission", 2, nethttp.MethodPost, base, b.body)
		p.call("update with "+b.name, 4, nethttp.MethodPut, base+"/5", b.body)
		p.call("update with "+b.name+" and a bad id", 4, nethttp.MethodPut, base+"/abc", b.body)
	}

	for _, b := range []struct{ name, body string }{
		{"bad json", `{"name":`}, {"empty object", `{}`}, {"no body", ``}, {"wrong type", `{"name":5,"expires_at":3}`}, {"array", `[]`}, {"null", `null`},
		{"a bad date", `{"name":"ci","expires_at":"yesterday"}`}, {"a null date", `{"name":"ci","expires_at":null}`},
		{"a long name", `{"name":"` + strings.Repeat("a", 101) + `"}`}, {"unknown field", `{"name":"ci","x":1}`},
	} {
		p.call("create a key with "+b.name, 4, nethttp.MethodPost, base+"/5/keys", b.body)
		p.call("create a key with "+b.name+" and a bad id", 4, nethttp.MethodPost, base+"/abc/keys", b.body)
	}

	for _, b := range []struct{ name, body string }{
		{"bad json", `{"role_ref":`}, {"empty object", `{}`}, {"no body", ``}, {"wrong type", `{"role_ref":5,"level":[],"scope_id":"x"}`},
		{"array", `[]`}, {"null", `null`}, {"a null scope", `{"role_ref":"sales","level":"dealer","scope_id":null}`},
		{"a long role", `{"role_ref":"` + strings.Repeat("a", 65) + `","level":"dealer"}`},
		{"a long level", `{"role_ref":"a","level":"` + strings.Repeat("a", 33) + `"}`},
		{"unknown field", `{"role_ref":"sales","level":"dealer","x":1}`},
	} {
		p.call("bind with "+b.name, 4, nethttp.MethodPost, base+"/5/bindings", b.body)
		p.call("bind with "+b.name+" and a bad id", 4, nethttp.MethodPost, base+"/abc/bindings", b.body)
	}

	// malformed ids: non-numeric, zero, negative, fractional, out of range and a second bad id
	for _, bad := range []string{"abc", "0", "-1", "1.5", "99999999999999999999"} {
		for _, c := range []req{
			{nethttp.MethodGet, base + "/" + bad, ""}, {nethttp.MethodPut, base + "/" + bad, save},
			{nethttp.MethodPost, base + "/" + bad + "/deactivate", ""}, {nethttp.MethodGet, base + "/" + bad + "/keys", ""},
			{nethttp.MethodPost, base + "/" + bad + "/keys", `{"name":"ci"}`}, {nethttp.MethodPost, base + "/" + bad + "/keys/3/revoke", ""},
			{nethttp.MethodPost, base + "/5/keys/" + bad + "/revoke", ""}, {nethttp.MethodPost, base + "/" + bad + "/keys/" + bad + "/revoke", ""},
			{nethttp.MethodGet, base + "/" + bad + "/access", ""}, {nethttp.MethodGet, base + "/" + bad + "/scopes", ""},
			{nethttp.MethodPost, base + "/" + bad + "/bindings", bind}, {nethttp.MethodDelete, base + "/" + bad + "/bindings/21", ""},
			{nethttp.MethodDelete, base + "/5/bindings/" + bad, ""}, {nethttp.MethodDelete, base + "/" + bad + "/bindings/" + bad, ""},
		} {
			p.call(c.method+" "+c.path, 1, c.method, c.path, c.body)
			p.call(c.method+" "+c.path+" with an empty body", 1, c.method, c.path, `{}`)
			p.call(c.method+" "+c.path+" without permission", 3, c.method, c.path, c.body)
			p.call(c.method+" "+c.path+" without token", 0, c.method, c.path, c.body)
		}
	}

	p.call("a delete of one account", 1, nethttp.MethodDelete, base+"/5", "")
	p.call("a put on the list", 1, nethttp.MethodPut, base, save)
	p.call("a get of the deactivation", 1, nethttp.MethodGet, base+"/5/deactivate", "")

	return p.recorded
}

func TestWirePin(t *testing.T) {
	got := record(t)
	file := filepath.Join("testdata", pinFile)

	if os.Getenv("WIRE_PIN_UPDATE") != "" {
		require.NoError(t, os.WriteFile(file, marshal(t, got), 0o600))

		return
	}

	raw, err := os.ReadFile(file)
	require.NoError(t, err, "no wire pin: generate it with WIRE_PIN_UPDATE=1")

	var want map[string]exchange

	require.NoError(t, json.Unmarshal(raw, &want))

	for name, g := range got {
		w, ok := want[name]
		if assert.True(t, ok, "case %q is not in %s", name, file) {
			assert.JSONEq(t, string(marshal(t, w)), string(marshal(t, g)), "wire pin %q moved (WIRE_PIN_UPDATE=1 regenerates %s)", name, file)
		}
	}

	for name := range want {
		_, ok := got[name]
		assert.True(t, ok, "case %q of %s is no longer recorded", name, file)
	}
}

// renamed is arv-next's wire in this module's names: tenants and owners, where arv-next said dealers and
// responsible persons.
var renamed = strings.NewReplacer(
	`"dealer_id":`, `"tenant_id":`, `"responsible_user_id":`, `"owner_id":`, `"dealer":`, `"tenant":`, `"responsible":`, `"owner":`,
	// the fake echoes the input as text, so the names are inside a string too
	`\"dealer_id\":`, `\"tenant_id\":`, `\"responsible_user_id\":`, `\"owner_id\":`,
)

func rename(t *testing.T, v any) any {
	t.Helper()

	raw, err := json.Marshal(v)
	require.NoError(t, err)

	var out any

	require.NoError(t, json.Unmarshal([]byte(renamed.Replace(string(raw))), &out))

	return out
}

// accepted lists the cases whose answer is not arv-next's, each with its reason: the wire differences of
// moving the service accounts into the module. The list only shrinks: a case that no longer differs must be
// taken out.
func accepted(name string, g, w exchange) (string, bool) {
	access := strings.Contains(g.Request.Path, "/access") || strings.Contains(g.Request.Path, "/scopes") ||
		strings.Contains(g.Request.Path, "/bindings") || strings.Contains(g.Request.Path, "/bindable-roles")

	switch {
	case access && w.Response.Status/100 == 2:
		return "a success of the access routes is the access module's own answer (canned in arv-next), tested in access/accesshttp", true
	case access && w.Response.Status == nethttp.StatusNotFound:
		return "an unknown account or binding is the access module's own not-found, tested in access/accesshttp", true
	case strings.Contains(g.Request.Path, "bindable-roles") && g.Response.Status != w.Response.Status:
		return "the bindable roles' query is the access module's own (BindableInput): a text, big or fractional scope, or no level, is a 422 where arv-next said 400; an unknown or non-positive place is the engine's 404 and 400", true
	}

	return "", false
}

func TestWireMatchesARV(t *testing.T) {
	var arv map[string]exchange

	raw, err := os.ReadFile(filepath.Join("testdata", arvFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &arv))

	got := record(t)

	var (
		same, differ int
		unexpected   []string
		reasons      = map[string]int{}
	)

	for name, g := range got {
		w, ok := arv[name]
		require.True(t, ok, "case %q is not in arv-next's pin", name)

		w.Request.Body = rename(t, w.Request.Body)
		w.Response.Body = rename(t, w.Response.Body)
		w.Request.Path = renamed.Replace(w.Request.Path)

		equal := string(marshal(t, g)) == string(marshal(t, w))
		why, listed := accepted(name, g, w)

		switch {
		case equal:
			same++
		case listed:
			differ++
			reasons[why]++
		default:
			unexpected = append(unexpected, name)
		}
	}

	sort.Strings(unexpected)
	t.Logf("%d cases answer as arv-next does, %d differ for a listed reason:", same, differ)

	for why, n := range reasons {
		t.Logf("  %4d  %s", n, why)
	}

	for _, name := range unexpected {
		w, g := arv[name], got[name]
		t.Errorf("%s differs from arv-next's pin:\narv-next: %s\nhere:     %s", name,
			asJSON(map[string]any{"status": w.Response.Status, "body": rename(t, w.Response.Body)}),
			asJSON(map[string]any{"status": g.Response.Status, "body": g.Response.Body}))
	}

	assert.Equal(t, len(arv), len(got), "every case of arv-next's pin is made")
	_ = fmt.Sprint
}

// The secret of a new key is shown once: never cached. An answer that is no key says nothing of caching.
func TestNewKeyIsNeverCached(t *testing.T) {
	r := router(t, held{1: {serviceaccount.Manage}})

	do := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, path, strings.NewReader(`{"name":"ci"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer u1")

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		return rec
	}

	ok := do("/api/v1/iam/service-accounts/5/keys")
	require.Equal(t, nethttp.StatusCreated, ok.Code)
	assert.Equal(t, "no-store", ok.Header().Get("Cache-Control"))

	failed := do("/api/v1/iam/service-accounts/404/keys")
	require.Equal(t, nethttp.StatusNotFound, failed.Code)
	assert.Empty(t, failed.Header().Get("Cache-Control"), "an error is not a secret")
}
