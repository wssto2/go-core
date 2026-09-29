package authzhttp_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/auth"
	authgorm "github.com/wssto2/go-core/auth/gormstore"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authzhttp"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/tenancy"
)

type testUser struct{ id, location int }

func (u testUser) GetID() int { return u.id }

func catalogue(t *testing.T) *authz.Catalogue {
	c := authz.NewCatalogue()
	require.NoError(t, c.Define("crm.lead:view", authz.Ownable("crm.lead")))
	require.NoError(t, c.Define("crm.offer:view"))
	require.NoError(t, c.Define("iam.user:manage"))
	return c
}

func world(t *testing.T) *authztest.World {
	roles := []authz.Role{
		{Key: "seller", Name: "Seller", Grants: []authz.Grant{
			{Permission: "crm.lead:view", Qualifier: authz.QualifierOwn},
			{Permission: "crm.offer:view", Qualifier: authz.QualifierAll},
		}},
		authz.ComputedRole("webmaster", "Webmaster", authz.All()),
	}
	w := authztest.NewWorld(t, catalogue(t), roles...)
	w.Places.AddDealer(1).AddDealer(2).AddLocation(10, 1)
	return w
}

func router(mw ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middlewares.ErrorHandler(slog.New(slog.DiscardHandler), nil, false))
	r.Use(func(ctx *gin.Context) { // stands in for auth.Authenticated
		if id := ctx.GetHeader("X-Test-User"); id != "" {
			n := 0
			for _, c := range id {
				n = n*10 + int(c-'0')
			}
			auth.SetUser(ctx, testUser{id: n})
		}
	})
	r.Use(authzhttp.Principals(func(_ context.Context, i auth.Identifiable) (authz.Principal, bool) {
		u, ok := i.(testUser)
		return authz.User(u.id, u.location), ok
	}))
	r.Use(mw...)
	return r
}

func do(t *testing.T, r http.Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestRequireAndRequireAny(t *testing.T) {
	w := world(t)
	w.Bind(authz.Subject{Kind: authz.KindUser, ID: 1}, "seller", authztest.Dealer(1))
	r := router()
	ok := func(ctx *gin.Context) { ctx.String(200, "ok") }
	r.GET("/offers", authzhttp.Require(w.Engine, "crm.offer:view"), ok)
	r.GET("/users", authzhttp.Require(w.Engine, "iam.user:manage"), ok)
	r.GET("/either", authzhttp.RequireAny(w.Engine, "iam.user:manage", "crm.lead:view"), ok)
	r.GET("/neither", authzhttp.RequireAny(w.Engine, "iam.user:manage"), ok)
	r.GET("/typo", authzhttp.Require(w.Engine, "crm.offer:fly"), ok)

	as1 := map[string]string{"X-Test-User": "1"}
	assert.Equal(t, 200, do(t, r, "/offers", as1).Code)
	assert.Equal(t, 403, do(t, r, "/users", as1).Code)
	assert.Equal(t, 200, do(t, r, "/either", as1).Code, "one of the permissions is enough")
	assert.Equal(t, 403, do(t, r, "/neither", as1).Code)
	assert.Equal(t, 500, do(t, r, "/typo", as1).Code, "an unknown permission is a bug, not a quiet 403")
	assert.Equal(t, 403, do(t, r, "/offers", map[string]string{"X-Test-User": "2"}).Code, "no bindings, no access")
	assert.Equal(t, 401, do(t, r, "/offers", nil).Code, "no identity")
}

func TestPrincipalsRejectsWhatItCannotResolve(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middlewares.ErrorHandler(slog.New(slog.DiscardHandler), nil, false))
	r.Use(func(ctx *gin.Context) { auth.SetUser(ctx, testUser{id: 1}) })
	r.Use(authzhttp.Principals(func(context.Context, auth.Identifiable) (authz.Principal, bool) { return authz.Principal{}, false }))
	r.GET("/", func(ctx *gin.Context) { ctx.String(200, "ok") })
	assert.Equal(t, 401, do(t, r, "/", nil).Code)
}

func TestPinTenant(t *testing.T) {
	w := world(t)
	w.Bind(authz.Subject{Kind: authz.KindUser, ID: 1}, "seller", authztest.Location(10)) // dealer 1
	w.Bind(authz.Subject{Kind: authz.KindUser, ID: 2}, "webmaster", authztest.Org())
	w.Bind(authz.Subject{Kind: authz.KindUser, ID: 3}, "seller", authztest.Dealer(1))
	w.Bind(authz.Subject{Kind: authz.KindUser, ID: 3}, "seller", authztest.Dealer(2))

	r := router(authzhttp.PinTenant(w.Engine))
	r.GET("/tenant", func(ctx *gin.Context) {
		id, ok := tenancy.TenantIDFromContext(ctx.Request.Context())
		if !ok {
			ctx.String(200, "none")
			return
		}
		ctx.String(200, "tenant %d", id)
	})
	assert.Equal(t, "tenant 1", do(t, r, "/tenant", map[string]string{"X-Test-User": "1"}).Body.String())
	assert.Equal(t, "none", do(t, r, "/tenant", map[string]string{"X-Test-User": "2"}).Body.String(), "the root crosses tenants")
	assert.Equal(t, "none", do(t, r, "/tenant", map[string]string{"X-Test-User": "3"}).Body.String(), "two tenants pin none")
	assert.Equal(t, "none", do(t, r, "/tenant", map[string]string{"X-Test-User": "4"}).Body.String())
}

func TestMeAccess(t *testing.T) {
	w := world(t)
	w.Bind(authz.Subject{Kind: authz.KindUser, ID: 1}, "seller", authztest.Dealer(1))
	r := router()
	r.GET("/me/access", authzhttp.MeAccess(w.Engine))

	rec := do(t, r, "/me/access", map[string]string{"X-Test-User": "1"})
	require.Equal(t, 200, rec.Code)
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Subject     authz.Subject `json:"subject"`
			Root        bool          `json:"root"`
			Permissions map[string]struct {
				Scope     authz.Scope     `json:"scope"`
				Qualifier authz.Qualifier `json:"qualifier"`
			} `json:"permissions"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.True(t, body.Success)
	assert.Equal(t, authz.Subject{Kind: authz.KindUser, ID: 1}, body.Data.Subject)
	assert.False(t, body.Data.Root)
	assert.Equal(t, authz.QualifierOwn, body.Data.Permissions["crm.lead:view"].Qualifier)
	assert.Equal(t, authztest.Dealer(1), body.Data.Permissions["crm.lead:view"].Scope)
	assert.Equal(t, authz.QualifierAll, body.Data.Permissions["crm.offer:view"].Qualifier)
	assert.NotContains(t, body.Data.Permissions, "iam.user:manage")
	assert.Contains(t, rec.Body.String(), `"qualifier":"own"`, "qualifiers serialize as words")

	assert.Equal(t, 401, do(t, r, "/me/access", nil).Code)
}

func TestServiceAccountsAuthenticateByAPIKey(t *testing.T) {
	w := world(t)
	sa := authz.ServiceAccount(5)
	w.Bind(sa.Subject, "seller", authztest.Dealer(1))

	db, cleanup, err := database.PrepareTestDB(&auth.APIKey{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cleanup() })
	keys := authgorm.NewGormAPIKeyStore(db)
	mint := func(owner int, mutate func(*auth.APIKey)) string {
		raw, err := auth.GenerateAPIKey()
		require.NoError(t, err)
		k := &auth.APIKey{UserID: owner, Name: "integration"}
		if mutate != nil {
			mutate(k)
		}
		require.NoError(t, keys.CreateKey(context.Background(), k, raw))
		return raw
	}
	good := mint(5, nil)
	revoked := mint(5, nil)
	unbound := mint(6, nil)
	past := time.Now().Add(-time.Hour)
	expired := mint(5, func(k *auth.APIKey) { k.ExpiresAt = &past })
	future := time.Now().Add(time.Hour)
	fresh := mint(5, func(k *auth.APIKey) { k.ExpiresAt = &future })
	require.NoError(t, db.Model(&auth.APIKey{}).Where("key_prefix = ?", revoked[:8]).Update("revoked", true).Error)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middlewares.ErrorHandler(slog.New(slog.DiscardHandler), nil, false))
	r.Use(authzhttp.ServiceAccounts(keys, ""))
	r.GET("/who", authzhttp.Require(w.Engine, "crm.offer:view"), func(ctx *gin.Context) {
		p, _ := authz.PrincipalFrom(ctx.Request.Context())
		ctx.String(200, p.String())
	})

	rec := do(t, r, "/who", map[string]string{"X-API-Key": good})
	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "service:5", rec.Body.String())
	assert.Equal(t, 200, do(t, r, "/who", map[string]string{"X-API-Key": fresh}).Code)
	assert.Equal(t, 401, do(t, r, "/who", nil).Code, "no key")
	assert.Equal(t, 401, do(t, r, "/who", map[string]string{"X-API-Key": "not-a-real-key"}).Code)
	assert.Equal(t, 401, do(t, r, "/who", map[string]string{"X-API-Key": revoked}).Code, "revoked")
	assert.Equal(t, 401, do(t, r, "/who", map[string]string{"X-API-Key": expired}).Code, "expired")
	assert.Equal(t, 401, do(t, r, "/who", map[string]string{"X-API-Key": strings.Repeat("a", 500)}).Code, "absurdly long")
	assert.Equal(t, 403, do(t, r, "/who", map[string]string{"X-API-Key": unbound}).Code, "a valid key of an account with no bindings")
}
