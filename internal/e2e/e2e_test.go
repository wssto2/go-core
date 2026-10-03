// Package e2e holds the test that installs go-core's modules together, the way
// an application's main does, and uses them through HTTP.
package e2e_test

import (
	"encoding/json"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/access/accesshttp"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/gormstore"
	"github.com/wssto2/go-core/identity/identitytest"
	"gorm.io/gorm"
)

// browser is an HTTP client that keeps cookies, like one.
type browser struct {
	t       *testing.T
	handler nethttp.Handler
	cookies map[string]*nethttp.Cookie
}

func (b *browser) do(method, path string, body any) (int, json.RawMessage) {
	b.t.Helper()

	reader := strings.NewReader("")

	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(b.t, err)

		reader = strings.NewReader(string(raw))
	}

	req := httptest.NewRequestWithContext(b.t.Context(), method, path, reader)
	req.Header.Set("Content-Type", "application/json")

	for _, c := range b.cookies {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, req)

	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)

	return rec.Code, envelope.Data
}

func (b *browser) signIn(login, password string) {
	b.t.Helper()

	status, data := b.do(nethttp.MethodPost, "/api/v1/auth/login", map[string]string{"login": login, "password": password})
	require.Equal(b.t, nethttp.StatusOK, status, string(data))
}

// me is the permissions of GET /api/v1/auth/me.
func (b *browser) me() map[string]json.RawMessage {
	b.t.Helper()

	status, data := b.do(nethttp.MethodGet, "/api/v1/auth/me", nil)
	require.Equal(b.t, nethttp.StatusOK, status, string(data))

	var payload struct {
		Access struct {
			Permissions map[string]json.RawMessage `json:"permissions"`
		} `json:"access"`
	}

	require.NoError(b.t, json.Unmarshal(data, &payload))

	return payload.Access.Permissions
}

// Identity and access installed as an application's main does:
//
//	app := gocore.New(cfg, gocore.WithPrefix("/api"))
//	users := identity.Install(app, identity.AllowImpersonation("iam.user:impersonate"))
//	access.Install(app, permissions, users)
//
// An administrator signs in and sees their permissions in /auth/me; somebody
// without a role may not edit roles; after the administrator binds a role to
// them, their /auth/me shows it. On SQLite, MySQL and MariaDB.
func TestIdentityAndAccessWorkTogether(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("local", db)

		opts := []gocore.Option{gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithPrefix("/api")}
		if db.Name() == "sqlite" {
			opts = append(opts, gocore.WithAutoMigrate(t.Context())) // what gocoretest.New sets
		}

		app := gocore.New(bootstrap.DefaultConfig(), opts...)

		permissions := authz.NewCatalogue()
		permissions.MustDefine("crm.customer:view")
		permissions.MustDefine("iam.user:impersonate", authz.Sensitive())

		users := identity.Install(app, identity.AllowImpersonation("iam.user:impersonate"))
		acc := access.Install(app, permissions, users, access.WithRoles(
			authz.Role{Key: "seller", Name: "Seller", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")},
			authz.ComputedRole("webmaster", "Webmaster", authz.All()),
		))

		if db.Name() != "sqlite" {
			require.NoError(t, app.Migrate(t.Context()), "both modules' migration files")
		}

		for _, a := range []struct {
			id    int
			login string
		}{{1, "admin"}, {2, "ines"}} {
			seed := identitytest.Account(a.id, a.login, "secret")
			seed.CreatedAt = identitytest.Epoch
			_, err := gormstore.New(db).Accounts.Create(t.Context(), seed)
			require.NoError(t, err)
		}

		require.NoError(t, acc.Seed(t.Context(), authz.Subject{Kind: authz.KindUser, ID: 1}, "webmaster"))
		require.NoError(t, app.Check())

		handler, err := app.Handler()
		require.NoError(t, err)

		admin := &browser{t: t, handler: handler, cookies: map[string]*nethttp.Cookie{}}
		ines := &browser{t: t, handler: handler, cookies: map[string]*nethttp.Cookie{}}

		admin.signIn("admin", "secret")
		assert.Contains(t, admin.me(), "iam.role:manage", "the administrator's access is in /auth/me")

		ines.signIn("ines", "secret")
		assert.Empty(t, ines.me(), "nobody gave ines a role yet")

		status, _ := ines.do(nethttp.MethodPost, "/api/v1/iam/roles", accesshttp.CreateRoleInput{
			Name: "Clerk", Grants: []accesshttp.GrantInput{{Permission: "crm.customer:view", Qualifier: "all"}},
		})
		assert.Equal(t, nethttp.StatusForbidden, status, "without a role she may not edit roles")

		status, data := admin.do(nethttp.MethodPost, "/api/v1/iam/users/2/bindings", accesshttp.BindInput{RoleRef: "seller", Level: "organization"})
		require.Equal(t, nethttp.StatusOK, status, string(data))

		assert.Contains(t, ines.me(), "crm.customer:view", "the role the administrator gave shows in her /auth/me")

		// Names come from identity: the SubjectDirectory is *identity.Users itself.
		status, data = admin.do(nethttp.MethodGet, "/api/v1/iam/users/2/access", nil)
		require.Equal(t, nethttp.StatusOK, status, string(data))
		assert.Contains(t, string(data), `"created_by":{"id":1,"name":"admin"}`, "who bound the role is named by identity")

		// Impersonation goes through the engine, with the permission the application named.
		status, _ = ines.do(nethttp.MethodPost, "/api/v1/auth/login-as", map[string]int{"user_id": 1})
		assert.Equal(t, nethttp.StatusForbidden, status)

		status, data = admin.do(nethttp.MethodPost, "/api/v1/auth/login-as", map[string]int{"user_id": 2})
		assert.Equal(t, nethttp.StatusOK, status, string(data))
	})
}
