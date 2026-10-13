package identity_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	nethttp "net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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
	t       testing.TB
	handler nethttp.Handler
	cookies []*nethttp.Cookie
}

func newClient(tb testing.TB, app *gocore.App) *client {
	tb.Helper()

	handler, err := app.Handler()
	require.NoError(tb, err)

	return &client{t: tb, handler: handler, cookies: nil}
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

func seed(tb testing.TB, db *gorm.DB, a account.Account) {
	tb.Helper()

	a.CreatedAt = gocoretest.Epoch
	_, err := gormstore.New(db).Accounts.Create(tb.Context(), a)
	require.NoError(tb, err)
}

// The whole thing, as a test of a feature uses it: install, sign in, call a route.
func TestInstallGivesTheApplicationItsAuthentication(t *testing.T) {
	app := newApp(t)
	users := identity.Install(app, identity.WithoutMail())

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
	app := newApp(t)
	engine := fakeAccess{authz.MyAccess{
		Subject:     authz.Subject{Kind: authz.KindUser, ID: 7},
		Permissions: map[string]authz.PermissionAccess{"tickets.ticket:view": {Scope: authz.Scope{Level: "organization"}, Qualifier: authz.QualifierAll}},
	}}

	identity.Install(app,
		identity.WithoutMail(),
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
		app := newApp(t) // read at request time

		opts := []identity.Option{identity.WithoutMail()}
		if allow {
			opts = append(opts, identity.AllowImpersonation("identity.account:impersonate"))
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
	app := newApp(t, gocoretest.Databases("local", "shared"))
	identity.Install(app, identity.WithoutMail(), identity.On(Shared))

	assert.True(t, app.Database(Shared).Migrator().HasTable("accounts"))
	assert.True(t, app.Database(Shared).Migrator().HasTable("user_signins"))
	assert.True(t, app.Database(Shared).Migrator().HasTable("tokens"))
	assert.False(t, app.Database().Migrator().HasTable("accounts"))

	require.NoError(t, app.Check())
}

func TestWithAccountsReplacesTheStore(t *testing.T) {
	app := newApp(t)
	accounts := identitytest.NewAccounts()

	identity.Install(app, identity.WithoutMail(), identity.WithAccounts(accounts), identity.WithHasher(identitytest.Hasher))

	_, err := accounts.Create(t.Context(), identitytest.Account(1, "ana", "secret"))
	require.NoError(t, err)

	c := newClient(t, app)
	assert.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"}).Code)
}

func TestTwoAuthenticatingFeaturesAreAStartUpProblem(t *testing.T) {
	app := newApp(t)
	identity.Install(app, identity.WithoutMail())
	identity.Install(app, identity.WithoutMail())

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

		opts := []gocore.Option{gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithAuthorizer(authztest.AllowAll())}
		if db.Name() == "sqlite" {
			opts = append(opts, gocore.WithAutoMigrate(t.Context()))
		}

		app := gocore.New(bootstrap.DefaultConfig(), opts...)

		cat := authz.NewCatalogue()
		require.NoError(t, identity.DefinePermissions(cat))
		app.Permissions(cat)

		identity.Install(app, identity.WithoutMail(), identity.On("local")) // AddConnection does not make a primary one

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
	app := newApp(t)
	hmac := auth.NewHMACHasher([]byte("secret"))

	identity.Install(app, identity.WithoutMail(), identity.WithRefreshHasher(hmac))
	seed(t, app.Database(), identitytest.Account(1, "ana", "secret"))

	c := newClient(t, app)
	require.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"}).Code)

	var stored struct{ RefreshToken string }
	require.NoError(t, app.Database().Table("tokens").Take(&stored).Error)
	assert.Len(t, stored.RefreshToken, 43, "an HMAC hash, not SHA-256 hex (64)")

	assert.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodPost, "/v1/auth/refresh", nil).Code)
}

// G2: with the application's own accounts store, identity creates no accounts table (nor its phone
// column and unique e-mail), on every database, but keeps the sign-in, token, code and reauth tables.
func TestWithAccountsCreatesNoAccountsTable(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, conn *gorm.DB) {
		t.Helper()
		//nolint:exhaustruct,exhaustruct_v5 // the registry's defaults
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("local", conn)

		opts := []gocore.Option{
			gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)),
			gocore.WithAuthorizer(authztest.AllowAll()),
		}
		if conn.Name() == "sqlite" {
			opts = append(opts, gocore.WithAutoMigrate(t.Context()))
		}

		app := gocore.New(bootstrap.DefaultConfig(), opts...)

		cat := authz.NewCatalogue()
		require.NoError(t, identity.DefinePermissions(cat))
		app.Permissions(cat)

		identity.Install(app, identity.WithoutMail(), identity.On("local"), identity.WithAccounts(identitytest.NewAccounts()))

		if conn.Name() != "sqlite" {
			require.NoError(t, app.Migrate(t.Context()))
		}

		tableNames := []string{"user_sign" + "ins", "tokens", "user_verification_codes", "user_reauth_attempts"}
		for _, table := range tableNames {
			assert.Truef(t, conn.Migrator().HasTable(table), "%s is identity's", table)
		}

		assert.False(t, conn.Migrator().HasTable("accounts"))
	})
}

// denied is the host's own refusal, which identity's authentication would word differently.
const denied = "denied"

// openSession opens a session for the account the way another application sharing the tokens table would.
func openSession(t *testing.T, app *gocore.App, accountID int) account.Credentials {
	t.Helper()

	now := time.Now()
	//nolint:exhaustruct,exhaustruct_v5 // no actor, device or address
	creds, err := gormstore.New(app.Database()).Sessions.Open(t.Context(), account.NewSession{
		AccountID: accountID, At: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	return creds
}

// G4: identity authenticates only the routes the application mounts behind it. A host's own
// routes, public to go-core and guarded by the host's chain over the same tokens table, keep
// working, and a session either side opened is good for both.
func TestAHostsOwnChainCoexistsWithIdentitysAuthentication(t *testing.T) {
	t.Parallel()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail())
	seed(t, app.Database(), identitytest.Account(7, "ana", "secret"))

	hostChain := func(ctx *gin.Context) {
		token, _ := strings.CutPrefix(ctx.GetHeader("Authorization"), "Bearer ")

		var row auth.Token

		err := app.Database().Where("token_value = ?", token).Take(&row).Error
		if err != nil || token == "" {
			ctx.JSON(nethttp.StatusUnauthorized, gin.H{"host": denied}) // the host's own refusal, not identity's

			return
		}

		ctx.JSON(nethttp.StatusOK, gin.H{"host": "ok", "user": row.UserID})
	}

	app.Routes(route.Raw(nethttp.MethodGet, "/host/ping").Public().To(hostChain))
	require.NoError(t, app.Check())

	web := newClient(t, app)
	unauthenticated := web.do(nethttp.MethodGet, "/host/ping", nil).Body.String()
	assert.JSONEq(t, `{"host":"`+denied+`"}`, unauthenticated, "identity does not authenticate the host's route")

	// a session identity opens works on the host's chain
	login := web.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"})
	require.Equal(t, nethttp.StatusOK, login.Code)

	var access string

	for _, ck := range login.Result().Cookies() {
		if ck.Name == "access_token" {
			access = ck.Value
		}
	}

	require.NotEmpty(t, access)

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/host/ping", nil)
	req.Header.Set("Authorization", "Bearer "+access)

	rec := httptest.NewRecorder()
	web.handler.ServeHTTP(rec, req)
	assert.JSONEq(t, `{"host":"ok","user":7}`, rec.Body.String())

	// and a session the host's side wrote to tokens works on identity's
	creds := openSession(t, app, 7)

	me := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/v1/auth/me", nil)
	me.Header.Set("Authorization", "Bearer "+creds.Access)

	rec = httptest.NewRecorder()
	web.handler.ServeHTTP(rec, me)
	assert.Equal(t, nethttp.StatusOK, rec.Code, rec.Body.String())
}

// R8: a session another application opened by login-as, named "login-as:<actor>|<device>" in
// tokens.name, is left by the return route, which signs in as the actor with no password.
func TestReturnFromALoginAsSessionTheHostNamed(t *testing.T) {
	t.Parallel()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail())
	seed(t, app.Database(), identitytest.Account(7, "ana", "secret"))
	seed(t, app.Database(), identitytest.Account(8, "bob", "secret"))

	creds := openSession(t, app, 8)

	marker := "login-as:7|Mozilla/5.0 (X11; Linux)"
	require.NoError(t, app.Database().Model(new(auth.Token)).
		Where("token_value = ?", creds.Access).Update("name", marker).Error)

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/v1/auth/login-as/return", nil)
	req.Header.Set("Authorization", "Bearer "+creds.Access)

	handler, err := app.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, nethttp.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"id":7`, "signed in as the actor")
	assert.NotContains(t, rec.Body.String(), "impersonator")

	old := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/v1/auth/me", nil)
	old.Header.Set("Authorization", "Bearer "+creds.Access)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, old)
	assert.Equal(t, nethttp.StatusUnauthorized, rec.Code, "the login-as session is over")
}

// R5 and R6: every login refusal has the one body shape (success, error, code), the inactive one too;
// signing in as an inactive person answers its own reason, not the not-found of a person who is not there.
func TestRefusalsHaveOneShapeAndLoginAsSaysInactive(t *testing.T) {
	t.Parallel()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.AllowImpersonation("iam.user:impersonate"))
	seed(t, app.Database(), identitytest.Account(7, "ana", "secret"))

	gone := identitytest.Account(8, "bob", "secret")
	gone.Active = false
	seed(t, app.Database(), gone)

	keys := func(rec *httptest.ResponseRecorder) []string {
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

		return slices.Sorted(maps.Keys(body))
	}

	attempt := func(password string) *httptest.ResponseRecorder {
		body := map[string]string{"login": "bob", "password": password}

		return newClient(t, app).do(nethttp.MethodPost, "/v1/auth/login", body)
	}

	inactive, wrong := attempt("secret"), attempt("nope")

	assert.Contains(t, inactive.Body.String(), "identity.signin.inactive")
	assert.Equal(t, keys(wrong), keys(inactive), "the same keys as a wrong password")

	web := newClient(t, app)
	login(t, web, "ana", "secret")

	rec := web.do(nethttp.MethodPost, "/v1/auth/login-as", map[string]int{"user_id": 8})
	assert.Equal(t, nethttp.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "identity.account.inactive")

	rec = web.do(nethttp.MethodPost, "/v1/auth/login-as", map[string]int{"user_id": 99})
	assert.Equal(t, nethttp.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "identity.account.not_found")
}

// a host's own Impersonation that refuses with a bare forbidden (no reason) still answers with go-core's.
type bareRefusal struct{ covers bool }

func (b bareRefusal) Permitted(context.Context, account.Account) error {
	if b.covers {
		return nil
	}

	return apperr.Forbidden("user does not have permission to login as another user")
}

func (b bareRefusal) Covers(context.Context, account.Account, account.Account) error {
	return apperr.Forbidden("target holds more than you")
}

func TestLoginAsRefusedForLackOfPermissionCarriesItsReason(t *testing.T) {
	t.Parallel()

	for name, imp := range map[string]bareRefusal{"permitted": {covers: false}, "covers": {covers: true}} {
		app := newApp(t)
		identity.Install(app, identity.WithoutMail(), identity.WithImpersonation(imp))
		seed(t, app.Database(), identitytest.Account(7, "ana", "secret"))
		seed(t, app.Database(), identitytest.Account(8, "bob", "secret"))

		c := newClient(t, app)
		login(t, c, "ana", "secret")

		rec := c.do(nethttp.MethodPost, "/v1/auth/login-as", map[string]int{"user_id": 8})
		assert.Equal(t, nethttp.StatusForbidden, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `"code":"authz.forbidden"`, name)
	}
}
