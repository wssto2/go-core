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

// TestScopeByTenant_NoTenantNoMarker_MatchesNothing verifies the scope fails
// closed: a context that never established a tenant reads no rows.
func TestScopeByTenant_NoTenantNoMarker_MatchesNothing(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, db.Create(&[]vehicle{{DealerID: 1}, {DealerID: 2}}).Error)

	var got []vehicle
	require.NoError(t, db.Scopes(tenancy.ScopeByTenant(context.Background(), "dealer_id")).Find(&got).Error)
	assert.Empty(t, got, "no tenant and no all-tenants marker must not see every row")

	stmt := db.Session(&gorm.Session{DryRun: true}).
		Scopes(tenancy.ScopeByTenant(context.Background(), "dealer_id")).
		Find(&vehicle{}).Statement
	assert.Contains(t, stmt.SQL.String(), "1 = 0")
}

// TestScopeByTenant_AllTenantsMarker_SeesEveryRow verifies the explicit marker
// is the only way to cross tenants.
func TestScopeByTenant_AllTenantsMarker_SeesEveryRow(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, db.Create(&[]vehicle{{DealerID: 1}, {DealerID: 2}}).Error)

	var got []vehicle
	ctx := tenancy.WithAllTenants(context.Background())
	require.NoError(t, db.Scopes(tenancy.ScopeByTenant(ctx, "dealer_id")).Find(&got).Error)
	assert.Len(t, got, 2)
	assert.True(t, tenancy.AllTenantsFromContext(ctx))
	assert.False(t, tenancy.AllTenantsFromContext(context.Background()))
}

// TestScopeByTenant_TenantWinsOverMarker verifies the narrower scope applies.
func TestScopeByTenant_TenantWinsOverMarker(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, db.Create(&[]vehicle{{DealerID: 1}, {DealerID: 2}}).Error)

	ctx := tenancy.WithTenantID(tenancy.WithAllTenants(context.Background()), 2)
	var got []vehicle
	require.NoError(t, db.Scopes(tenancy.ScopeByTenant(ctx, "dealer_id")).Find(&got).Error)
	require.Len(t, got, 1)
	assert.Equal(t, 2, got[0].DealerID)
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

	// the all-tenants marker does not satisfy it
	_, err = tenancy.RequireTenantScope(tenancy.WithAllTenants(context.Background()), "dealer_id")
	assert.Error(t, err)
}
