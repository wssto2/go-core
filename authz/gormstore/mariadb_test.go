package gormstore_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/authz/storetest"
	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
)

// These tests run against a real MariaDB (10.3 in production) and skip unless
// AUTHZ_MARIADB_DSN is set; see the authz package documentation.

// freshTables drops the authz and audit tables, then creates them the way an
// application would: by running MySQLSchema, or by calling Migrate.
func freshTables(t *testing.T, db *gorm.DB, viaSQL bool) {
	t.Helper()
	require.NoError(t, db.Exec("SET FOREIGN_KEY_CHECKS = 0").Error)
	for _, table := range []string{"role_bindings", "role_permissions", "roles", "audit_logs"} {
		require.NoError(t, db.Exec("DROP TABLE IF EXISTS "+table).Error)
	}
	if viaSQL {
		for _, stmt := range strings.Split(gormstore.MySQLSchema, ";") {
			if strings.TrimSpace(stmt) != "" {
				require.NoError(t, db.Exec(stmt).Error, stmt)
			}
		}
	} else {
		require.NoError(t, gormstore.Migrate(db))
	}
	require.NoError(t, audit.Migrate(db))
}

func TestMariaDBStoreConforms(t *testing.T) {
	db, ok := authztest.MariaDB(t)
	if !ok {
		t.Skip(authztest.MariaDBEnv + " is not set")
	}
	for name, viaSQL := range map[string]bool{"MySQLSchema": true, "Migrate": false} {
		for _, audited := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "", true: "/audited"}[audited], func(t *testing.T) {
				storetest.Run(t, func(t *testing.T) authz.Store {
					freshTables(t, db, viaSQL)
					if audited {
						return gormstore.New(db, gormstore.WithAudit(audit.NewRepository(database.NewTransactor(db))))
					}
					return gormstore.New(db)
				})
			})
		}
	}
}

func TestMariaDBEnforcesTheBindingCheckConstraint(t *testing.T) {
	db, ok := authztest.MariaDB(t)
	if !ok {
		t.Skip(authztest.MariaDBEnv + " is not set")
	}
	for _, viaSQL := range []bool{true, false} {
		freshTables(t, db, viaSQL)
		insert := func(roleID int, roleKey string) error {
			return db.Exec(`INSERT INTO role_bindings (subject_kind, subject_id, role_id, role_key, scope_level, scope_id, created_at)
				VALUES ('user', 1, ?, ?, 'dealer', 3, NOW())`, roleID, roleKey).Error
		}
		assert.Error(t, insert(5, "seller"), "both role references")
		assert.Error(t, insert(0, ""), "no role reference")
		assert.NoError(t, insert(5, ""))
		assert.NoError(t, insert(0, "seller"))
		assert.Error(t, insert(5, ""), "the unique index refuses a duplicate")
	}
}

// Every writer takes the role's row lock first (SELECT ... FOR UPDATE), so a
// delete racing updates either wins whole or loses whole.
func TestMariaDBRoleLockUnderContention(t *testing.T) {
	db, ok := authztest.MariaDB(t)
	if !ok {
		t.Skip(authztest.MariaDBEnv + " is not set")
	}
	freshTables(t, db, true)
	s := gormstore.New(db, gormstore.WithAudit(audit.NewRepository(database.NewTransactor(db))))
	ctx := context.Background()
	actor := authz.Subject{Kind: authz.KindUser, ID: 1}
	role, err := s.SaveRole(ctx, actor, authz.Role{Name: "r", Grants: authz.Grants(authz.QualifierAll, "a.b:view")})
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := range 10 {
		wg.Go(func() {
			if i == 9 {
				errs[i] = s.DeleteRole(ctx, actor, role.ID)
				return
			}
			r := role
			r.Grants = authz.Grants(authz.QualifierAll, "a.b:view", "a.b:update")
			_, errs[i] = s.SaveRole(ctx, actor, r)
		})
	}
	wg.Wait()
	for i, err := range errs { // an update after the delete finds no role; nothing else may fail
		if err != nil {
			assert.ErrorIs(t, err, authz.ErrRoleNotFound, "writer %d", i)
		}
	}
	// no orphaned grants either way
	var orphans int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM role_permissions rp LEFT JOIN roles r ON r.id = rp.role_id WHERE r.id IS NULL").Scan(&orphans).Error)
	assert.Zero(t, orphans)
}

func TestMariaDBEngineOverGormStore(t *testing.T) {
	db, ok := authztest.MariaDB(t)
	if !ok {
		t.Skip(authztest.MariaDBEnv + " is not set")
	}
	freshTables(t, db, true)
	engineOverGormStore(t, db)
}
