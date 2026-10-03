package gormstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/authz/migrations"
	"github.com/wssto2/go-core/authz/storetest"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"gorm.io/gorm"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, cleanup := database.MustPrepareTestDB()
	t.Cleanup(cleanup)
	require.NoError(t, gormstore.Migrate(db))
	require.NoError(t, audit.Migrate(db))
	return db
}

func TestStoreConformsWithoutAudit(t *testing.T) {
	storetest.Run(t, func(t *testing.T) authz.Store { return gormstore.New(newDB(t)) })
}

func TestStoreConformsWithAudit(t *testing.T) {
	storetest.Run(t, func(t *testing.T) authz.Store {
		db := newDB(t)
		return gormstore.New(db, gormstore.WithAudit(audit.NewRepository(database.NewTransactor(db))))
	})
}

func TestAuditRowsCarryBeforeAndAfter(t *testing.T) {
	db := newDB(t)
	s := gormstore.New(db, gormstore.WithAudit(audit.NewRepository(database.NewTransactor(db))))
	ctx := context.Background()
	actor := authz.Subject{Kind: authz.KindUser, ID: 42}

	role, err := s.SaveRole(ctx, actor, authz.Role{Name: "Sales", Grants: authz.Grants(authz.QualifierAll, "crm.offer:view")})
	require.NoError(t, err)
	b, err := s.Bind(ctx, actor, authz.Binding{Subject: authz.Subject{Kind: authz.KindUser, ID: 7}, Role: authz.RoleRef{ID: role.ID}, Scope: authz.Scope{Level: "dealer", ID: 3}})
	require.NoError(t, err)
	role.Grants = append(role.Grants, authz.Grant{Permission: "crm.lead:view", Qualifier: authz.QualifierOwn})
	role.Name = "Sales v2"
	_, err = s.SaveRole(ctx, actor, role)
	require.NoError(t, err)
	require.NoError(t, s.Unbind(ctx, actor, b.ID))
	require.NoError(t, s.DeleteRole(ctx, actor, role.ID))

	var logs []audit.AuditLog
	require.NoError(t, db.Order("id").Find(&logs).Error)
	type row struct{ entity, action string }
	var got []row
	for _, l := range logs {
		assert.Equal(t, 42, l.ActorID)
		got = append(got, row{l.EntityType, l.Action})
	}
	assert.Equal(t, []row{
		{gormstore.EntityRole, "create"},
		{gormstore.EntityBinding, "create"},
		{gormstore.EntityRole, "update"},
		{gormstore.EntityBinding, "delete"},
		{gormstore.EntityRole, "delete"},
	}, got)

	update := logs[2]
	assert.JSONEq(t, `{"name":"Sales","description":"","grants":["crm.offer:view=all"],"attrs":null}`, string(update.BeforeState))
	assert.JSONEq(t, `{"name":"Sales v2","description":"","grants":["crm.lead:view=own","crm.offer:view=all"],"attrs":null}`, string(update.AfterState))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(update.Metadata, &meta))
	assert.Equal(t, []any{"user:7"}, meta["affected"], "a custom-role change lists the users it affects")
	assert.Equal(t, "user", meta["actor_kind"])

	assert.JSONEq(t, `{"subject":"user:7","role":"#1","scope":"dealer:3"}`, string(logs[1].AfterState))
	assert.Equal(t, "null", string(logs[1].BeforeState))
}

type failingAudit struct{}

func (failingAudit) Write(context.Context, audit.Entry) error { return errors.New("audit down") }

func TestFailedAuditRollsTheChangeBack(t *testing.T) {
	db := newDB(t)
	s := gormstore.New(db, gormstore.WithAudit(failingAudit{}))
	ctx := context.Background()
	actor := authz.Subject{Kind: authz.KindUser, ID: 1}

	_, err := s.SaveRole(ctx, actor, authz.Role{Name: "x", Grants: authz.Grants(authz.QualifierAll, "a.b:view")})
	require.Error(t, err)
	roles, err := s.ListRoles(ctx)
	require.NoError(t, err)
	assert.Empty(t, roles, "no change without its audit record")

	_, err = s.Bind(ctx, actor, authz.Binding{Subject: authz.Subject{Kind: authz.KindUser, ID: 2}, Role: authz.RoleRef{Key: "seller"}, Scope: authz.Scope{Level: "dealer", ID: 3}})
	require.Error(t, err)
	got, err := s.BindingsFor(ctx, authz.Subject{Kind: authz.KindUser, ID: 2})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDatabaseRefusesABindingWithBothRoleReferences(t *testing.T) {
	db := newDB(t)
	err := db.Exec(`INSERT INTO role_bindings (subject_kind, subject_id, role_id, role_key, scope_level, scope_id, created_at)
		VALUES ('user', 1, 5, 'seller', 'dealer', 3, CURRENT_TIMESTAMP)`).Error
	assert.Error(t, err, "the CHECK constraint keeps a role reference to exactly one of ID and key")
	err = db.Exec(`INSERT INTO role_bindings (subject_kind, subject_id, role_id, role_key, scope_level, scope_id, created_at)
		VALUES ('user', 1, 0, '', 'dealer', 3, CURRENT_TIMESTAMP)`).Error
	assert.Error(t, err)
}

// The store reads and writes on behalf of an engine: a full round trip through
// Admin and Engine over the real tables.
func TestEngineOverGormStore(t *testing.T) { engineOverGormStore(t, newDB(t)) }

func engineOverGormStore(t *testing.T, db *gorm.DB) {
	t.Helper()
	store := gormstore.New(db, gormstore.WithAudit(audit.NewRepository(database.NewTransactor(db))))

	cat := authz.NewCatalogue()
	require.NoError(t, cat.Define("iam.role:manage"))
	require.NoError(t, cat.Define("iam.user:manage"))
	require.NoError(t, cat.Define("crm.offer:view", authz.Ownable("crm.offer")))
	places := authztest.NewPlaces().AddDealer(1).AddLocation(10, 1)
	engine, err := authz.NewEngine(authz.Config{
		Catalogue: cat, Hierarchy: authztest.Hierarchy(), Store: store, Resolver: places,
		Roles: []authz.Role{authz.ComputedRole("webmaster", "Webmaster", authz.All())},
	})
	require.NoError(t, err)
	admin, err := authz.NewAdmin(authz.AdminConfig{Engine: engine, Store: store, ManageRoles: "iam.role:manage", ManageBindings: "iam.user:manage", Protected: []string{"iam.user:manage"}})
	require.NoError(t, err)

	root := authz.Subject{Kind: authz.KindUser, ID: 1}
	_, err = store.Bind(context.Background(), root, authz.Binding{Subject: root, Role: authz.RoleRef{Key: "webmaster"}, Scope: authztest.Org()})
	require.NoError(t, err)
	rootCtx := authz.WithPrincipal(context.Background(), authz.Principal{Subject: root})

	role, err := admin.SaveRole(rootCtx, authz.Role{Name: "Viewer", Grants: authz.Grants(authz.QualifierOwn, "crm.offer:view")})
	require.NoError(t, err)
	worker := authz.Subject{Kind: authz.KindUser, ID: 2}
	workerCtx := authz.WithPrincipal(context.Background(), authz.Principal{Subject: worker})
	_, err = admin.Bind(rootCtx, authz.Binding{Subject: worker, Role: role.Ref(), Scope: authztest.Location(10)})
	require.NoError(t, err)

	assert.NoError(t, engine.RequireOn(workerCtx, "crm.offer:view", authz.Resource{Scope: authztest.Location(10), Owner: 2}))
	assert.ErrorIs(t, engine.RequireOn(workerCtx, "crm.offer:view", authz.Resource{Scope: authztest.Location(10), Owner: 3}), authz.ErrForbidden)

	role.Grants = authz.Grants(authz.QualifierAll, "crm.offer:view")
	_, err = admin.SaveRole(rootCtx, role)
	require.NoError(t, err)
	assert.NoError(t, engine.RequireOn(workerCtx, "crm.offer:view", authz.Resource{Scope: authztest.Location(10), Owner: 3}), "the role change applies at once")
}

// MySQLSchema stays in step with the models: same tables, same columns.
func TestMySQLSchemaMatchesModels(t *testing.T) {
	db := newDB(t)
	tables := regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS (\w+) \((.*?)\) ENGINE`).FindAllStringSubmatch(gormstore.MySQLSchema, -1)
	require.Len(t, tables, 3)
	colDef := regexp.MustCompile(`^\s+(\w+) (?:INT|VARCHAR|TEXT|DATETIME)`)
	for _, m := range tables {
		var want []string
		for _, line := range regexp.MustCompile(`\n`).Split(m[2], -1) {
			if c := colDef.FindStringSubmatch(line); c != nil {
				want = append(want, c[1])
			}
		}
		cols, err := db.Migrator().ColumnTypes(m[1])
		require.NoError(t, err, m[1])
		var got []string
		for _, c := range cols {
			got = append(got, c.Name())
		}
		assert.ElementsMatch(t, want, got, m[1])
	}
}

// The holder queries run on SQLite, MySQL and MariaDB. The server targets get
// their tables from the real migration files, SQLite from the models.
func TestStoreHoldersOnEveryDatabase(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		storetest.RunHolders(t, func(t *testing.T) storetest.HolderStore {
			for _, table := range []string{"role_bindings", "role_permissions", "roles"} {
				require.NoError(t, db.Exec("DROP TABLE IF EXISTS "+table).Error)
			}
			if db.Name() == "sqlite" {
				require.NoError(t, gormstore.Migrate(db))
			} else {
				reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
				reg.AddConnection("scratch", db)
				require.NoError(t, db.Exec("DROP TABLE IF EXISTS goose_db_version").Error)
				require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Up(t.Context()))
			}
			return gormstore.New(db)
		})
	})
}
