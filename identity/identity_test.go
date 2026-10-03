package identity_test

import (
	"context"
	"encoding/json"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/gormstore"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/navigation"
	"github.com/wssto2/go-core/route"
	"gorm.io/gorm"
)

const Shared database.Connection = "shared"

// Whoami is a feature's own route: behind identity's authentication, acting as the signed-in person.
var Whoami = route.Get[route.None, int]("/whoami")

func whoami(ctx context.Context, _ route.None) (int, error) {
	p, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return 0, apperr.Unauthorized("no principal")
	}

	return p.ID, nil
}

type client struct {
	t       *testing.T
	handler nethttp.Handler
	cookies []*nethttp.Cookie
}

func newClient(t *testing.T, app *gocore.App) *client {
	t.Helper()

	handler, err := app.Handler()
	require.NoError(t, err)

	return &client{t: t, handler: handler}
}

func (c *client) do(method, path string, body any) *httptest.ResponseRecorder {
	c.t.Helper()

	var reader *strings.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(c.t, err)

		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}

	req := httptest.NewRequestWithContext(c.t.Context(), method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "test-agent")

	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}

	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)

	// like a browser: keep the cookies the server sets, drop the ones it expires
	for _, ck := range rec.Result().Cookies() {
		kept := c.cookies[:0]

		for _, old := range c.cookies {
			if old.Name != ck.Name {
				kept = append(kept, old)
			}
		}

		c.cookies = kept
		if ck.MaxAge >= 0 {
			c.cookies = append(c.cookies, ck)
		}
	}

	return rec
}

func seed(t *testing.T, db *gorm.DB, a account.Account) {
	t.Helper()

	a.CreatedAt = gocoretest.Epoch
	_, err := gormstore.New(db).Accounts.Create(t.Context(), a)
	require.NoError(t, err)
}

// The whole thing, as a test of a feature uses it: install, sign in, call a route.
func TestInstallGivesTheApplicationItsAuthentication(t *testing.T) {
	app := gocoretest.New(t)
	users := identity.Install(app)

	app.Routes(Whoami.To(whoami))
	seed(t, app.Database(), identitytest.Account(7, "ana", "secret"))

	require.NoError(t, app.Check())

	c := newClient(t, app)

	assert.Equal(t, nethttp.StatusUnauthorized, c.do(nethttp.MethodGet, "/whoami", nil).Code, "a route is behind the authentication")

	login := c.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"})
	require.Equal(t, nethttp.StatusOK, login.Code, login.Body.String())

	who := c.do(nethttp.MethodGet, "/whoami", nil)
	assert.Equal(t, nethttp.StatusOK, who.Code)
	assert.JSONEq(t, `{"success":true,"data":7}`, who.Body.String(), "the handler acts as the signed-in person")

	acc, err := users.Get(t.Context(), 7)
	require.NoError(t, err)
	assert.Equal(t, "ana", acc.Login)

	require.Equal(t, nethttp.StatusNoContent, c.do(nethttp.MethodPost, "/v1/auth/logout", nil).Code)
	assert.Equal(t, nethttp.StatusUnauthorized, c.do(nethttp.MethodGet, "/whoami", nil).Code)
}

func TestTheOptionsShapeThePayload(t *testing.T) {
	app := gocoretest.New(t)
	engine := fakeAccess{authz.MyAccess{
		Subject:     authz.Subject{Kind: authz.KindUser, ID: 7},
		Permissions: map[string]authz.PermissionAccess{"tickets.ticket:view": {Scope: authz.Scope{Level: "organization"}, Qualifier: authz.QualifierAll}},
	}}

	identity.Install(app,
		identity.WithAccess(engine),
		identity.WithNavigation(
			navigation.Node{I18n: "nav.tickets", Route: "tickets", Permissions: []string{"tickets.ticket:view"}},
			navigation.Node{I18n: "nav.roles", Route: "roles", Permissions: []string{"access.role:view"}},
		),
		identity.WithUserProjector(func(_ context.Context, a account.Account) (any, error) {
			return map[string]any{"id": a.ID, "name": a.Name}, nil
		}),
	)
	seed(t, app.Database(), identitytest.Account(7, "ana", "secret"))

	c := newClient(t, app)

	login := c.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"})
	require.Equal(t, nethttp.StatusOK, login.Code, login.Body.String())

	var got struct {
		Data struct {
			User       map[string]any `json:"user"`
			Access     map[string]any `json:"access"`
			Navigation []any          `json:"navigation"`
		} `json:"data"`
	}

	require.NoError(t, json.Unmarshal(login.Body.Bytes(), &got))
	assert.Equal(t, map[string]any{"id": float64(7), "name": "ana"}, got.Data.User)
	assert.Contains(t, got.Data.Access["permissions"], "tickets.ticket:view")
	assert.Len(t, got.Data.Navigation, 1, "the menu is cut to what the person holds")
}

type fakeAccess struct{ held authz.MyAccess }

func (f fakeAccess) MyAccess(context.Context) (authz.MyAccess, error) { return f.held, nil }

func TestImpersonationIsOffUnlessAllowed(t *testing.T) {
	for _, allow := range []bool{false, true} {
		app := gocoretest.New(t)

		opts := []identity.Option{}
		if allow {
			opts = append(opts, identity.AllowImpersonation(authztest.AllowAll(), "identity.account:impersonate"))
		}

		identity.Install(app, opts...)
		seed(t, app.Database(), identitytest.Account(1, "ana", "secret"))
		seed(t, app.Database(), identitytest.Account(2, "boris", "hunter2"))

		c := newClient(t, app)
		require.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"}).Code)

		r := c.do(nethttp.MethodPost, "/v1/auth/login-as", map[string]int{"user_id": 2})
		if allow {
			assert.Equal(t, nethttp.StatusOK, r.Code, r.Body.String())
		} else {
			assert.Equal(t, nethttp.StatusForbidden, r.Code)
		}
	}
}

func TestOnPutsTheTablesOnThatConnection(t *testing.T) {
	app := gocoretest.New(t, gocoretest.Databases("local", "shared"))
	identity.Install(app, identity.On(Shared))

	assert.True(t, app.Database(Shared).Migrator().HasTable("accounts"))
	assert.True(t, app.Database(Shared).Migrator().HasTable("user_signins"))
	assert.True(t, app.Database(Shared).Migrator().HasTable("tokens"))
	assert.False(t, app.Database().Migrator().HasTable("accounts"))

	require.NoError(t, app.Check())
}

func TestWithAccountsReplacesTheStore(t *testing.T) {
	app := gocoretest.New(t)
	accounts := identitytest.NewAccounts()

	identity.Install(app, identity.WithAccounts(accounts), identity.WithHasher(identitytest.Hasher))

	_, err := accounts.Create(t.Context(), identitytest.Account(1, "ana", "secret"))
	require.NoError(t, err)

	c := newClient(t, app)
	assert.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"}).Code)
}

func TestTwoAuthenticatingFeaturesAreAStartUpProblem(t *testing.T) {
	app := gocoretest.New(t)
	identity.Install(app)
	identity.Install(app)

	err := app.Check()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "two features set how requests are authenticated")
}

// Install end to end on every database: SQLite from the models, MySQL and
// MariaDB from the migration files, the way each is deployed.
func TestInstallEndToEndOnEveryDatabase(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("local", db)

		opts := []gocore.Option{gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler))}
		if db.Name() == "sqlite" {
			opts = append(opts, gocore.WithAutoMigrate(t.Context()))
		}

		app := gocore.New(bootstrap.DefaultConfig(), opts...)
		identity.Install(app, identity.On("local")) // AddConnection does not make a primary one

		if db.Name() != "sqlite" {
			require.NoError(t, app.Migrate(t.Context()), "the module's migration files")
		}

		seed(t, db, identitytest.Account(1, "ana", "secret"))

		c := newClient(t, app)

		login := c.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "Ana", "password": "secret"})
		require.Equal(t, nethttp.StatusOK, login.Code, login.Body.String())
		require.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodGet, "/v1/auth/me", nil).Code)

		refresh := c.do(nethttp.MethodPost, "/v1/auth/refresh", nil)
		require.Equal(t, nethttp.StatusOK, refresh.Code, refresh.Body.String())
		require.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodGet, "/v1/auth/me", nil).Code, "the rotated cookies work")

		for range 5 {
			bad := &client{t: t, handler: c.handler}
			bad.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "wrong"})
		}

		locked := (&client{t: t, handler: c.handler}).do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"})
		assert.Equal(t, nethttp.StatusUnprocessableEntity, locked.Code)
		assert.Contains(t, locked.Body.String(), "identity.signin.locked")
	})
}

// An application whose sessions are hashed with another hasher keeps them.
func TestWithRefreshHasherReadsSessionsStoredThatWay(t *testing.T) {
	app := gocoretest.New(t)
	hmac := auth.NewHMACHasher([]byte("secret"))

	identity.Install(app, identity.WithRefreshHasher(hmac))
	seed(t, app.Database(), identitytest.Account(1, "ana", "secret"))

	c := newClient(t, app)
	require.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"}).Code)

	var stored struct{ RefreshToken string }
	require.NoError(t, app.Database().Table("tokens").Take(&stored).Error)
	assert.Len(t, stored.RefreshToken, 43, "an HMAC hash, not SHA-256 hex (64)")

	assert.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodPost, "/v1/auth/refresh", nil).Code)
}
