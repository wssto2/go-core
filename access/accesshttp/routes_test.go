package accesshttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access/accesshttp"
	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
)

// people is the SubjectDirectory: users 1 to 9.
type people struct{}

func (people) SubjectNames(_ context.Context, subjects []authz.Subject) (map[authz.Subject]string, error) {
	out := map[authz.Subject]string{}

	for _, s := range subjects {
		if s.Kind == authz.KindUser && s.ID >= 1 && s.ID <= 9 {
			out[s] = "Person " + strconv.Itoa(s.ID)
		}
	}

	return out, nil
}

type server struct {
	t      *testing.T
	handle http.Handler
	store  *gormstore.Store
	engine *authz.Engine
}

// newServer mounts the routes under prefix on an application that signs in the
// user named by the X-User header. Dealers 10 and 20 exist.
func newServer(t *testing.T, prefix string) *server {
	t.Helper()

	cat := authz.NewCatalogue()
	for _, id := range admin.PermissionIDs() {
		cat.MustDefine(id)
	}

	cat.MustDefine("crm.customer:view")
	cat.MustDefine("system.job:run", authz.System())

	reg, cleanup := database.NewTestRegistry("local")
	t.Cleanup(func() { _ = cleanup() })

	db := reg.MustGet("local")
	require.NoError(t, gormstore.Migrate(db))

	store := gormstore.New(db)
	places := authztest.NewPlaces().AddDealer(10).AddDealer(20)

	engine, err := authz.NewEngine(authz.Config{
		Catalogue: cat, Hierarchy: authztest.Hierarchy(), Store: store, Resolver: places,
		Roles: []authz.Role{
			{Key: "seller", Name: "Seller", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")},
			{Key: "admin", Name: "Administrator", Grants: authz.Grants(authz.QualifierAll,
				"iam.role:view", "iam.role:manage", "iam.role:delete", "iam.user:view", "iam.user:manage")},
			authz.ComputedRole("webmaster", "Webmaster", authz.All()),
		},
	})
	require.NoError(t, err)

	scopes := catalogueOf{places}
	roles, bindings, err := admin.New(admin.Config{
		Engine: engine, Store: store, Scopes: scopes, Subjects: people{},
		Transactor: database.NewTransactor(db),
	})
	require.NoError(t, err)

	app := gocore.New(bootstrap.DefaultConfig(),
		gocore.WithPrefix(prefix), gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithAuthentication(func(c *gin.Context) {
			id, err := strconv.Atoi(c.GetHeader("X-User"))
			if err != nil {
				_ = c.Error(authz.Deny(""))
				c.AbortWithStatus(http.StatusUnauthorized)

				return
			}

			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.User(id, 0)))
			c.Next()
		}),
	)
	app.Authorize(engine)
	app.Permissions(cat)
	app.Routes(accesshttp.Declare().To(roles, bindings, engine)...)

	handle, err := app.Handler()
	require.NoError(t, err)

	return &server{t: t, handle: handle, store: store, engine: engine}
}

type catalogueOf struct{ *authztest.Places }

func (catalogueOf) Names(_ context.Context, scopes []authz.Scope) (map[authz.Scope]string, error) {
	out := map[authz.Scope]string{}

	for _, s := range scopes {
		if s.Level == "dealer" && (s.ID == 10 || s.ID == 20) {
			out[s] = "Dealer " + strconv.Itoa(s.ID)
		}
	}

	return out, nil
}

func (catalogueOf) Options(context.Context) ([]admin.ScopeOption, error) {
	return []admin.ScopeOption{
		{Scope: authztest.Dealer(10), Name: "Dealer 10", Parent: authztest.Org()},
		{Scope: authztest.Dealer(20), Name: "Dealer 20", Parent: authztest.Org()},
	}, nil
}

func (s *server) seed(user int, role string, scope authz.Scope) {
	s.t.Helper()
	_, err := s.store.Bind(context.Background(), authz.Subject{}, authz.Binding{
		Subject: authz.Subject{Kind: authz.KindUser, ID: user}, Role: authz.RoleRef{Key: role}, Scope: scope,
	})
	require.NoError(s.t, err)
	s.engine.EvictAll()
}

type reply struct {
	Status int
	Data   json.RawMessage `json:"data"`
	// Code is the stable reason of a refusal, for example authz.last_admin.
	Code string `json:"code"`
}

func (s *server) do(user int, method, path string, body any) reply {
	s.t.Helper()

	var reader *bytes.Reader

	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(s.t, err)

		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequestWithContext(s.t.Context(), method, path, reader)
	req.Header.Set("Content-Type", "application/json")

	if user > 0 {
		req.Header.Set("X-User", strconv.Itoa(user))
	}

	rec := httptest.NewRecorder()
	s.handle.ServeHTTP(rec, req)

	var out reply

	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	out.Status = rec.Code

	return out
}

func decode[T any](t *testing.T, r reply) T {
	t.Helper()

	var v T

	require.NoError(t, json.Unmarshal(r.Data, &v), string(r.Data))

	return v
}

func TestRolesOverHTTP(t *testing.T) {
	s := newServer(t, "")
	s.seed(1, "webmaster", authztest.Org())
	s.seed(2, "seller", authztest.Dealer(10))

	t.Run("listing needs sign-in and the view permission", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, s.do(0, "GET", "/v1/iam/roles", nil).Status)
		assert.Equal(t, http.StatusForbidden, s.do(2, "GET", "/v1/iam/roles", nil).Status)
	})

	t.Run("a custom role is built, shown, listed, changed and deleted", func(t *testing.T) {
		created := s.do(1, "POST", "/v1/iam/roles", accesshttp.CreateRoleInput{
			Name: "Clerk", Description: "reads", Grants: []accesshttp.GrantInput{{Permission: "crm.customer:view", Qualifier: "all"}},
		})
		require.Less(t, created.Status, 300, string(created.Data))

		role := decode[accesshttp.Role](t, created)
		assert.Equal(t, accesshttp.Role{
			Ref: role.Ref, ID: role.ID, Name: "Clerk", Description: "reads", Attrs: []accesshttp.Constraint{}, PermissionCount: 1,
			Grants: []accesshttp.Grant{{Permission: "crm.customer:view", Qualifier: "all"}},
		}, role)
		require.NotNil(t, role.ID)
		assert.Nil(t, role.Key)

		list := decode[accesshttp.RoleList](t, s.do(1, "GET", "/v1/iam/roles", nil))
		require.Len(t, list.Roles, 4)
		assert.Equal(t, "webmaster", list.Roles[0].Ref)
		assert.Nil(t, list.Roles[0].ID)
		assert.True(t, list.Roles[0].Predefined && list.Roles[0].Computed)

		updated := s.do(1, "PUT", "/v1/iam/roles/"+role.Ref, accesshttp.CreateRoleInput{
			Name: "Clerk 2", Grants: []accesshttp.GrantInput{{Permission: "crm.customer:view", Qualifier: "all"}},
		})
		assert.Equal(t, "Clerk 2", decode[accesshttp.Role](t, updated).Name)

		show := decode[accesshttp.Role](t, s.do(1, "GET", "/v1/iam/roles/"+role.Ref, nil))
		assert.Equal(t, "Clerk 2", show.Name)

		assert.Equal(t, http.StatusNoContent, s.do(1, "DELETE", "/v1/iam/roles/"+role.Ref, nil).Status)
		assert.Equal(t, http.StatusNotFound, s.do(1, "GET", "/v1/iam/roles/"+role.Ref, nil).Status)
	})

	t.Run("bad input is told what is wrong", func(t *testing.T) {
		missing := s.do(1, "POST", "/v1/iam/roles", map[string]any{"description": "no name"})
		assert.Equal(t, http.StatusUnprocessableEntity, missing.Status, string(missing.Data))

		qualifier := s.do(1, "POST", "/v1/iam/roles", accesshttp.CreateRoleInput{
			Name: "x", Grants: []accesshttp.GrantInput{{Permission: "crm.customer:view", Qualifier: "everyone"}},
		})
		assert.Equal(t, http.StatusBadRequest, qualifier.Status)

		unknown := s.do(1, "POST", "/v1/iam/roles", accesshttp.CreateRoleInput{
			Name: "x", Grants: []accesshttp.GrantInput{{Permission: "no.such:perm", Qualifier: "all"}},
		})
		assert.Equal(t, http.StatusBadRequest, unknown.Status)
		assert.Equal(t, "authz.invalid", unknown.Code)
	})

	t.Run("a predefined role is read-only", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, s.do(1, "PUT", "/v1/iam/roles/seller", accesshttp.CreateRoleInput{Name: "x"}).Status)
		assert.Equal(t, http.StatusForbidden, s.do(1, "DELETE", "/v1/iam/roles/seller", nil).Status)
	})
}

func TestHoldersCompareAndReplaceOverHTTP(t *testing.T) {
	s := newServer(t, "")
	s.seed(1, "webmaster", authztest.Org())

	clerk := decode[accesshttp.Role](t, s.do(1, "POST", "/v1/iam/roles", accesshttp.CreateRoleInput{
		Name: "Clerk", Grants: []accesshttp.GrantInput{{Permission: "crm.customer:view", Qualifier: "all"}, {Permission: "iam.user:view", Qualifier: "all"}},
	}))
	require.Less(t, s.do(1, "POST", "/v1/iam/users/5/bindings", accesshttp.BindInput{RoleRef: clerk.Ref, Level: "dealer", ScopeID: new(10)}).Status, 300)

	holders := decode[accesshttp.RoleHolders](t, s.do(1, "GET", "/v1/iam/roles/"+clerk.Ref+"/holders", nil))
	require.Len(t, holders.Holders, 1)
	assert.Equal(t, accesshttp.RoleHolder{
		Subject: accesshttp.SubjectRef{Kind: "user", ID: 5}, Name: "Person 5",
		Scope: accesshttp.Scope{Level: "dealer", ID: new(10), Name: new("Dealer 10")},
	}, holders.Holders[0])

	cmp := decode[accesshttp.RoleComparison](t, s.do(1, "GET", "/v1/iam/roles/"+clerk.Ref+"/compare?with=seller", nil))
	assert.Equal(t, []accesshttp.Grant{{Permission: "iam.user:view", Qualifier: "all"}}, cmp.OnlyInRole)
	assert.Empty(t, cmp.OnlyInOther)
	assert.Empty(t, cmp.Different)
	assert.Equal(t, http.StatusUnprocessableEntity, s.do(1, "GET", "/v1/iam/roles/"+clerk.Ref+"/compare", nil).Status, "with is required")

	replaced := decode[accesshttp.Replaced](t, s.do(1, "POST", "/v1/iam/roles/"+clerk.Ref+"/replace", accesshttp.ReplaceRoleInput{With: "seller"}))
	assert.Equal(t, 1, replaced.Rebound)
	assert.Empty(t, decode[accesshttp.RoleHolders](t, s.do(1, "GET", "/v1/iam/roles/"+clerk.Ref+"/holders", nil)).Holders)
}

func TestBindingsOverHTTP(t *testing.T) {
	s := newServer(t, "")
	s.seed(1, "webmaster", authztest.Org())
	s.seed(2, "admin", authztest.Dealer(10))

	bound := s.do(2, "POST", "/v1/iam/users/5/bindings", accesshttp.BindInput{RoleRef: "seller", Level: "dealer", ScopeID: new(10)})
	require.Less(t, bound.Status, 300, string(bound.Data))

	binding := decode[accesshttp.Binding](t, bound)
	assert.Equal(t, "seller", binding.Role.Ref)
	assert.Equal(t, accesshttp.Scope{Level: "dealer", ID: new(10), Name: new("Dealer 10")}, binding.Scope)
	assert.Equal(t, &accesshttp.PersonRef{ID: 2, Name: "Person 2"}, binding.CreatedBy)

	t.Run("refusals carry their reason", func(t *testing.T) {
		other := s.do(2, "POST", "/v1/iam/users/5/bindings", accesshttp.BindInput{RoleRef: "seller", Level: "dealer", ScopeID: new(20)})
		assert.Equal(t, http.StatusForbidden, other.Status)

		self := s.do(2, "POST", "/v1/iam/users/2/bindings", accesshttp.BindInput{RoleRef: "seller", Level: "dealer", ScopeID: new(10)})
		assert.Equal(t, http.StatusForbidden, self.Status)
		assert.Equal(t, "authz.self_assignment", self.Code)

		system := s.do(1, "POST", "/v1/iam/roles", accesshttp.CreateRoleInput{Name: "System", Grants: []accesshttp.GrantInput{{Permission: "system.job:run", Qualifier: "all"}}})
		ref := decode[accesshttp.Role](t, system).Ref
		escalate := s.do(2, "POST", "/v1/iam/users/5/bindings", accesshttp.BindInput{RoleRef: ref, Level: "dealer", ScopeID: new(10)})
		assert.Equal(t, "authz.escalation", escalate.Code)

		own, err := s.store.BindingsFor(context.Background(), authz.Subject{Kind: authz.KindUser, ID: 2})
		require.NoError(t, err)
		require.Len(t, own, 1)

		last := s.do(2, "DELETE", "/v1/iam/users/2/bindings/"+strconv.Itoa(own[0].ID), nil)
		assert.Equal(t, http.StatusForbidden, last.Status, "the dealer administrator's own last binding")
		assert.Equal(t, "authz.last_admin", last.Code)

		assert.Equal(t, http.StatusNotFound, s.do(2, "GET", "/v1/iam/users/42/access", nil).Status)
	})

	t.Run("access explains and scopes and bindable roles follow the actor", func(t *testing.T) {
		access := decode[accesshttp.SubjectAccess](t, s.do(2, "GET", "/v1/iam/users/5/access", nil))
		assert.Equal(t, accesshttp.SubjectRef{Kind: "user", ID: 5}, access.Subject)
		assert.True(t, access.CanManage)
		require.Len(t, access.Effective, 1)
		assert.Equal(t, "crm.customer:view", access.Effective[0].Permission)
		assert.Equal(t, "seller", access.Effective[0].Grants[0].RoleKey)

		scopes := decode[accesshttp.ScopeOptions](t, s.do(2, "GET", "/v1/iam/users/5/scopes", nil))
		assert.Equal(t, accesshttp.ScopeOptions{RootLevel: "organization", Root: false, Places: []accesshttp.ScopeOption{
			{Level: "dealer", ID: 10, Name: "Dealer 10", ParentLevel: "organization"},
		}}, scopes)

		bindable := decode[accesshttp.BindableRoles](t, s.do(2, "GET", "/v1/iam/bindable-roles?level=dealer&scope_id=10", nil))
		require.NotEmpty(t, bindable.Roles)
		assert.Equal(t, "admin", bindable.Roles[0].Ref)
		assert.Equal(t, http.StatusUnprocessableEntity, s.do(2, "GET", "/v1/iam/bindable-roles", nil).Status)
		assert.Equal(t, http.StatusBadRequest, s.do(2, "GET", "/v1/iam/bindable-roles?level=dealer", nil).Status, "a place below the root needs its ID")

		assert.Equal(t, http.StatusNoContent, s.do(2, "DELETE", "/v1/iam/users/5/bindings/"+strconv.Itoa(binding.ID), nil).Status)
	})

	t.Run("a person without the permission is refused", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, s.do(5, "GET", "/v1/iam/users/2/access", nil).Status)
	})
}

func TestMyAccessStaysAndRoutesFollowThePrefix(t *testing.T) {
	s := newServer(t, "/api")
	s.seed(1, "webmaster", authztest.Org())

	assert.Equal(t, http.StatusNotFound, s.do(1, "GET", "/v1/iam/roles", nil).Status)
	assert.Equal(t, http.StatusOK, s.do(1, "GET", "/api/v1/iam/roles", nil).Status)

	var mine authz.MyAccess

	body := s.do(1, "GET", "/api/v1/me/access", nil)
	require.Equal(t, http.StatusOK, body.Status)
	require.NoError(t, json.Unmarshal(body.Data, &mine))
	assert.True(t, mine.Root)
	assert.Contains(t, mine.Permissions, "iam.role:manage")
}
