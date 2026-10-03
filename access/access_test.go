package access_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/access/accesshttp"
	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authzts"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/gocore"
	"gorm.io/gorm"
)

// team is the SubjectDirectory of the tests: people 1 to 9.
type team struct{}

func (team) SubjectNames(_ context.Context, subjects []authz.Subject) (map[authz.Subject]string, error) {
	out := map[authz.Subject]string{}

	for _, s := range subjects {
		if s.Kind == authz.KindUser && s.ID >= 1 && s.ID <= 9 {
			out[s] = "Person " + strconv.Itoa(s.ID)
		}
	}

	return out, nil
}

// permissions is an application catalogue that knows nothing of the module.
func permissions() *authz.Catalogue {
	c := authz.NewCatalogue()
	c.MustDefine("crm.customer:view")
	c.MustDefine("crm.lead:view", authz.Ownable("crm.lead"))
	c.MustDefine("system.job:run", authz.System())

	return c
}

func roles() []authz.Role {
	return []authz.Role{
		{Key: "seller", Name: "Seller", Grants: []authz.Grant{
			{Permission: "crm.customer:view", Qualifier: authz.QualifierAll},
			{Permission: "crm.lead:view", Qualifier: authz.QualifierOwn},
		}},
		authz.ComputedRole("webmaster", "Webmaster", authz.All()),
	}
}

// signedIn signs in the user named by the X-User header.
func signedIn(c *gin.Context) {
	id, err := strconv.Atoi(c.GetHeader("X-User"))
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)

		return
	}

	c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.User(id, 0)))
	c.Next()
}

const local database.Connection = "local"

// newApp is an application on the given database with the module installed on
// the "local" connection. migrate says how the tables come to be.
// opts makes the app create SQLite tables from the models (what gocoretest.New
// does); MySQL and MariaDB run the module's files with app.Migrate.
func opts(db *gorm.DB) []gocore.Option {
	if db.Name() == "sqlite" {
		return []gocore.Option{gocore.WithAutoMigrate(context.Background())}
	}

	return nil
}

func newApp(db *gorm.DB, install func(*gocore.App) *access.Access) (*gocore.App, *access.Access) {
	reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
	reg.AddConnection(string(local), db)

	app := gocore.New(bootstrap.DefaultConfig(),
		append(opts(db), gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithAuthentication(signedIn), gocore.WithPrefix("/api"))...)

	return app, install(app)
}

func call(t testing.TB, app *gocore.App, user int, method, path string, body any) (int, json.RawMessage) {
	t.Helper()

	handler, err := app.Handler()
	require.NoError(t, err)

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User", strconv.Itoa(user))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)

	return rec.Code, envelope.Data
}

// A fresh application without tenancy: Install defines the module's
// permissions, the tables come from the real migration files (SQLite: the
// models), the first administrator is seeded, and roles and bindings are
// administered over HTTP. It runs on every database the module supports.
func TestAnApplicationWithoutTenancyAdministersRolesAndBindings(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		app, acc := newApp(db, func(app *gocore.App) *access.Access {
			return access.Install(app, permissions(), team{}, access.WithRoles(roles()...), access.On(local))
		})

		if db.Name() != "sqlite" {
			require.NoError(t, app.Migrate(t.Context()), "the module's own migrations create the tables")
		}

		require.NoError(t, acc.Seed(t.Context(), authz.Subject{Kind: authz.KindUser, ID: 1}, "webmaster"))
		require.NoError(t, acc.Seed(t.Context(), authz.Subject{Kind: authz.KindUser, ID: 1}, "webmaster"), "seeding twice is harmless")

		status, data := call(t, app, 1, "POST", "/api/v1/iam/roles", accesshttp.CreateRoleInput{
			Name: "Clerk", Grants: []accesshttp.GrantInput{{Permission: "crm.customer:view", Qualifier: "all"}},
		})
		require.Equal(t, http.StatusOK, status, string(data))

		var clerk accesshttp.Role

		require.NoError(t, json.Unmarshal(data, &clerk))

		status, data = call(t, app, 1, "POST", "/api/v1/iam/users/5/bindings", accesshttp.BindInput{RoleRef: clerk.Ref, Level: "organization"})
		require.Equal(t, http.StatusOK, status, string(data))

		status, data = call(t, app, 1, "GET", "/api/v1/iam/users/5/access", nil)
		require.Equal(t, http.StatusOK, status)

		var access accesshttp.SubjectAccess

		require.NoError(t, json.Unmarshal(data, &access))
		require.Len(t, access.Effective, 1)
		assert.Equal(t, "crm.customer:view", access.Effective[0].Permission)
		assert.Equal(t, accesshttp.Scope{Level: "organization"}, access.Effective[0].Grants[0].Scope)

		status, data = call(t, app, 1, "GET", "/api/v1/iam/users/5/scopes", nil)
		require.Equal(t, http.StatusOK, status)
		assert.JSONEq(t, `{"root":true,"places":[]}`, string(data), "no tenancy: the organization is the only place")

		status, _ = call(t, app, 5, "GET", "/api/v1/iam/roles", nil)
		assert.Equal(t, http.StatusForbidden, status, "the clerk may not read roles")

		status, data = call(t, app, 5, "GET", "/api/v1/me/access", nil)
		require.Equal(t, http.StatusOK, status)
		assert.Contains(t, string(data), "crm.customer:view")

		status, _ = call(t, app, 1, "POST", "/api/v1/iam/users/5/bindings", accesshttp.BindInput{RoleRef: "seller", Level: "dealer", ScopeID: new(3)})
		assert.Equal(t, http.StatusBadRequest, status, "there is no dealer level")
	})
}

func TestInstallDefinesTheModulesPermissionsAndTheUnionStaysGenerated(t *testing.T) {
	cat := permissions()
	db, cleanup := database.MustPrepareTestDB()
	t.Cleanup(cleanup)

	_, acc := newApp(db, func(app *gocore.App) *access.Access {
		return access.Install(app, cat, team{}, access.On(local))
	})
	require.NotNil(t, acc.Engine)

	ts := authzts.Render(cat, authzts.Options{})
	for _, id := range admin.PermissionIDs() {
		assert.Contains(t, ts, `| '`+id+`'`, "the generated permission union has it")
	}

	manage, ok := cat.Lookup("iam.user:manage")
	require.True(t, ok)
	assert.True(t, manage.Sensitive)
	assert.Equal(t, []string{"iam.user:view"}, manage.Requires)
}

func TestInstallReportsWhatIsMissingAndHowToFixIt(t *testing.T) {
	problems := func(install func(*gocore.App)) string {
		db, cleanup := database.MustPrepareTestDB()
		defer cleanup()

		app, _ := newApp(db, func(app *gocore.App) *access.Access {
			install(app)

			return nil
		})

		var startup *gocore.StartupError

		err := app.Check()
		require.ErrorAs(t, err, &startup)

		return err.Error()
	}

	t.Run("no users", func(t *testing.T) {
		msg := problems(func(app *gocore.App) { access.Install(app, permissions(), nil) })
		assert.Contains(t, msg, "SubjectDirectory")
		assert.Contains(t, msg, "Fix: pass the users")
	})

	t.Run("a role that grants an unknown permission", func(t *testing.T) {
		msg := problems(func(app *gocore.App) {
			access.Install(app, permissions(), team{}, access.On(local),
				access.WithRoles(authz.Role{Key: "ghost", Name: "Ghost", Grants: authz.Grants(authz.QualifierAll, "no.such:perm")}))
		})
		assert.Contains(t, msg, `predefined role "ghost"`)
	})

	t.Run("a feature without a resolver", func(t *testing.T) {
		cat := permissions()
		cat.MustDefine("auction.lot:view", authz.Feature("auction"))

		msg := problems(func(app *gocore.App) { access.Install(app, cat, team{}, access.On(local)) })
		assert.Contains(t, msg, "access.WithFeatures")
	})

	t.Run("a permission the catalogue cannot take", func(t *testing.T) {
		cat := permissions()
		_, err := authz.NewEngine(authz.Config{Catalogue: cat, Hierarchy: mustLevels(), Store: gormstore.New(nil), Resolver: access.NoScopes()})
		require.NoError(t, err) // freezes the catalogue

		msg := problems(func(app *gocore.App) { access.Install(app, cat, team{}, access.On(local)) })
		assert.Contains(t, msg, "frozen")
		assert.Contains(t, msg, "define the five permissions yourself before Install")
	})

	t.Run("an unregistered connection", func(t *testing.T) {
		msg := problems(func(app *gocore.App) { access.Install(app, permissions(), team{}, access.On("elsewhere")) })
		assert.Contains(t, msg, "elsewhere")
	})
}

func mustLevels() *authz.Hierarchy {
	h, _ := authz.NewHierarchy("organization")

	return h
}

func TestSeedNeedsAnInstalledAccessAndAKnownRole(t *testing.T) {
	assert.ErrorContains(t, (&access.Access{}).Seed(t.Context(), authz.Subject{Kind: authz.KindUser, ID: 1}, "webmaster"), "Install did not build")

	db, cleanup := database.MustPrepareTestDB()
	t.Cleanup(cleanup)
	require.NoError(t, gormstore.Migrate(db))

	_, acc := newApp(db, func(app *gocore.App) *access.Access {
		return access.Install(app, permissions(), team{}, access.On(local))
	})

	err := acc.Seed(t.Context(), authz.Subject{Kind: authz.KindUser, ID: 1}, "webmaster")
	require.Error(t, err)
	assert.ErrorContains(t, err, "access.WithRoles")
	assert.False(t, errors.Is(err, authz.ErrDuplicateBinding))
}

// Role and binding changes are audited, in the transaction of the change, when
// the application gives the module a repository.
func TestChangesAreAuditedWhenAskedFor(t *testing.T) {
	db, cleanup := database.MustPrepareTestDB()
	t.Cleanup(cleanup)
	require.NoError(t, gormstore.Migrate(db))
	require.NoError(t, audit.Migrate(db))

	app, acc := newApp(db, func(app *gocore.App) *access.Access {
		return access.Install(app, permissions(), team{}, access.On(local), access.WithRoles(roles()...),
			access.WithAudit(audit.NewRepository(database.NewTransactor(db))))
	})
	require.NoError(t, acc.Seed(t.Context(), authz.Subject{Kind: authz.KindUser, ID: 1}, "webmaster"))

	status, data := call(t, app, 1, "POST", "/api/v1/iam/users/5/bindings", accesshttp.BindInput{RoleRef: "seller", Level: "organization"})
	require.Equal(t, http.StatusOK, status, string(data))

	var logs []audit.AuditLog

	require.NoError(t, db.Order("id").Find(&logs).Error)
	require.Len(t, logs, 2, "the seeded administrator and the binding")
	assert.Zero(t, logs[0].ActorID, "a seed has no acting person")
	assert.Equal(t, "authz.binding", logs[1].EntityType)
	assert.Equal(t, "create", logs[1].Action)
	assert.Equal(t, 1, logs[1].ActorID, "by the person who gave the role")
	assert.True(t, strings.Contains(string(logs[1].AfterState), "seller"))
}
