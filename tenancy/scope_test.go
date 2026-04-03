package tenancy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/tenancy"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type vehicle struct {
	ID       uint
	DealerID int
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&vehicle{}))
	return db
}

// TestScopeByTenant_AppliesWhereClause verifies that the scope correctly
// filters by the tenant column using dialect-aware quoting.
func TestScopeByTenant_AppliesWhereClause(t *testing.T) {
	db := openTestDB(t)
	ctx := tenancy.WithTenantID(context.Background(), 42)

	// DryRun lets us inspect the generated SQL without executing.
	stmt := db.Session(&gorm.Session{DryRun: true}).
		Scopes(tenancy.ScopeByTenant(ctx, "dealer_id")).
		Find(&vehicle{}).Statement

	sql := stmt.SQL.String()
	assert.Contains(t, sql, "= ?", "scope should add a WHERE clause")
	assert.Equal(t, []interface{}{42}, stmt.Vars, "tenant ID must be a bind variable")
}

// TestScopeByTenant_NoTenant_IsNoop verifies that when no tenant is in context
// no WHERE clause is added.
func TestScopeByTenant_NoTenant_IsNoop(t *testing.T) {
	db := openTestDB(t)

	stmt := db.Session(&gorm.Session{DryRun: true}).
		Scopes(tenancy.ScopeByTenant(context.Background(), "dealer_id")).
		Find(&vehicle{}).Statement

	sql := stmt.SQL.String()
	assert.NotContains(t, sql, "dealer_id", "no-tenant scope should not add a WHERE clause")
}

func TestRequireTenantScope_AppliesWhereClause(t *testing.T) {
	db := openTestDB(t)
	ctx := tenancy.WithTenantID(context.Background(), 7)

	scope, err := tenancy.RequireTenantScope(ctx, "dealer_id")
	require.NoError(t, err)

	stmt := db.Session(&gorm.Session{DryRun: true}).
		Scopes(scope).
		Find(&vehicle{}).Statement

	assert.Contains(t, stmt.SQL.String(), "= ?")
	assert.Equal(t, []interface{}{7}, stmt.Vars)
}

func TestRequireTenantScope_NoTenant_ReturnsError(t *testing.T) {
	_, err := tenancy.RequireTenantScope(context.Background(), "dealer_id")
	assert.Error(t, err)
}

