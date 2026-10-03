package admin_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/authz/migrations"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"gorm.io/gorm"
)

// The permissions the administration is guarded by, the fixture's own ids.
var perms = admin.DefaultPermissions

func catalogue() *authz.Catalogue {
	c := authz.NewCatalogue()
	must := func(id string, opts ...authz.DefineOption) { c.MustDefine(id, opts...) }
	must("iam.role:view")
	must("iam.role:manage", authz.Sensitive(), authz.Requires("iam.role:view"))
	must("iam.role:delete", authz.Sensitive(), authz.Requires("iam.role:view"))
	must("iam.user:view")
	must("iam.user:manage", authz.Sensitive(), authz.Requires("iam.user:view"))
	must("crm.lead:view", authz.Ownable("crm.lead"))
	must("crm.customer:view")
	must("system.job:run", authz.System())
	must("report.group:view", authz.OrganizationOnly())
	return c
}

func predefined() []authz.Role {
	return []authz.Role{
		{Key: "seller", Name: "Seller", Grants: []authz.Grant{
			{Permission: "crm.lead:view", Qualifier: authz.QualifierOwn},
			{Permission: "crm.customer:view", Qualifier: authz.QualifierAll},
		}},
		{Key: "salesmanager", Name: "Sales manager", Grants: authz.Grants(authz.QualifierAll, "crm.lead:view", "crm.customer:view")},
		{Key: "dealeradmin", Name: "Dealer administrator", Grants: authz.Grants(authz.QualifierAll, "iam.user:view", "iam.user:manage")},
		{Key: "roleadmin", Name: "Role administrator", Grants: authz.Grants(authz.QualifierAll,
			"iam.role:view", "iam.role:manage", "iam.role:delete", "iam.user:view")},
		{Key: "distribution", Name: "Distribution", Grants: authz.Grants(authz.QualifierAll, "report.group:view")},
		authz.ComputedRole("webmaster", "Webmaster", authz.All()),
	}
}

// directory is both the SubjectDirectory and the ScopeCatalog of the fixture:
// dealers 10 and 20, locations 101 (of 10) and 201 (of 20), people 1 to 9.
type directory struct {
	*authztest.Places
	mu     sync.Mutex
	people map[authz.Subject]string
}

func newDirectory() *directory {
	d := &directory{Places: authztest.NewPlaces(), people: map[authz.Subject]string{}}
	d.AddDealer(10).AddDealer(20).AddLocation(101, 10).AddLocation(201, 20)
	for id, name := range map[int]string{1: "Ana", 2: "Boris", 3: "Cvita", 4: "Dino", 5: "Eva", 6: "Fran", 7: "Goran", 8: "Hana", 9: "Ivo"} {
		d.people[authz.Subject{Kind: authz.KindUser, ID: id}] = name
	}
	d.people[authz.Subject{Kind: authz.KindServiceAccount, ID: 1}] = "Importer bot"
	return d
}

func (d *directory) SubjectNames(_ context.Context, subjects []authz.Subject) (map[authz.Subject]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[authz.Subject]string{}
	for _, s := range subjects {
		if name, ok := d.people[s]; ok {
			out[s] = name
		}
	}
	return out, nil
}

var places = map[authz.Scope]string{
	authztest.Dealer(10):    "North",
	authztest.Dealer(20):    "South",
	authztest.Location(101): "North main street",
	authztest.Location(201): "South harbour",
}

func (d *directory) Names(_ context.Context, scopes []authz.Scope) (map[authz.Scope]string, error) {
	out := map[authz.Scope]string{}
	for _, s := range scopes {
		if name, ok := places[s]; ok {
			out[s] = name
		}
	}
	return out, nil
}

func (d *directory) Options(context.Context) ([]admin.ScopeOption, error) {
	return []admin.ScopeOption{
		{Scope: authztest.Dealer(10), Name: "North", Parent: authztest.Org()},
		{Scope: authztest.Location(101), Name: "North main street", Parent: authztest.Dealer(10)},
		{Scope: authztest.Dealer(20), Name: "South", Parent: authztest.Org()},
		{Scope: authztest.Location(201), Name: "South harbour", Parent: authztest.Dealer(20)},
	}, nil
}

type fixture struct {
	t        testing.TB
	Engine   *authz.Engine
	Store    *gormstore.Store
	Roles    *admin.Roles
	Bindings *admin.Bindings
}

// newFixture builds the services on a SQLite database whose tables come from the
// store's GORM models (test only; the real SQL is tested on MySQL and MariaDB).
func newFixture(t testing.TB) *fixture {
	t.Helper()
	f, cleanup, err := buildFixture()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	f.t = t
	return f
}

func buildFixture() (*fixture, func(), error) {
	db, cleanup := database.MustPrepareTestDB()
	if err := gormstore.Migrate(db); err != nil {
		cleanup()
		return nil, nil, err
	}
	f, err := fixtureOn(db)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return f, cleanup, nil
}

// onEveryDatabase runs fn on SQLite, MySQL and MariaDB (the server targets skip
// unless GOCORE_MYSQL_DSN / GOCORE_MARIADB_DSN are set). Server targets get
// their tables from the real migration files, SQLite from the models.
func onEveryDatabase(t *testing.T, fn func(t *testing.T, f *fixture)) {
	t.Helper()
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		if db.Dialector.Name() == "sqlite" {
			require.NoError(t, gormstore.Migrate(db))
		} else {
			reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
			reg.AddConnection("scratch", db)
			require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Up(t.Context()))
		}
		f, err := fixtureOn(db)
		require.NoError(t, err)
		f.t = t
		fn(t, f)
	})
}

func fixtureOn(db *gorm.DB) (*fixture, error) {
	store := gormstore.New(db)
	dir := newDirectory()

	engine, err := authz.NewEngine(authz.Config{
		Catalogue: catalogue(), Hierarchy: authztest.Hierarchy(), Roles: predefined(), Store: store, Resolver: dir,
	})
	if err != nil {
		return nil, err
	}

	roles, bindings, err := admin.New(admin.Config{
		Engine: engine, Store: store, Scopes: dir, Subjects: dir, Permissions: perms,
		Transactor: database.NewTransactor(db),
	})
	if err != nil {
		return nil, err
	}

	return &fixture{Engine: engine, Store: store, Roles: roles, Bindings: bindings}, nil
}

func user(id int) authz.Subject { return authz.Subject{Kind: authz.KindUser, ID: id} }

// as is a request of the person with that id.
func as(id int) context.Context { return authz.WithPrincipal(context.Background(), authz.User(id, 0)) }

// seed binds without any delegation check, the way a first administrator is made.
func (f *fixture) seed(id int, role string, scope authz.Scope) authz.Binding {
	f.t.Helper()
	b, err := f.Store.Bind(context.Background(), authz.Subject{}, authz.Binding{Subject: user(id), Role: authz.RoleRef{Key: role}, Scope: scope})
	require.NoError(f.t, err)
	f.Engine.Evict(user(id))
	return b
}

func (f *fixture) seedCustom(id, roleID int, scope authz.Scope) authz.Binding {
	f.t.Helper()
	b, err := f.Store.Bind(context.Background(), authz.Subject{}, authz.Binding{Subject: user(id), Role: authz.RoleRef{ID: roleID}, Scope: scope})
	require.NoError(f.t, err)
	f.Engine.Evict(user(id))
	return b
}

func (f *fixture) seedCustomExample(id, roleID int, scope authz.Scope) {
	if _, err := f.Store.Bind(context.Background(), authz.Subject{}, authz.Binding{Subject: user(id), Role: authz.RoleRef{ID: roleID}, Scope: scope}); err != nil {
		panic(err)
	}
	f.Engine.Evict(user(id))
}
