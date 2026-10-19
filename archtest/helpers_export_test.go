package archtest_test

import (
	"go/parser"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/archtest"
)

// The helpers an application's own guards would otherwise copy are exported.
func TestExportedHelpers(t *testing.T) {
	t.Parallel()

	require.True(t, archtest.InTree("a/b/c.go", "a/b"))
	require.True(t, archtest.InTree("a/b", "a/b"))
	require.False(t, archtest.InTree("a/bc/d.go", "a/b"))

	lit, err := parser.ParseExpr(`"x"`)
	require.NoError(t, err)

	value, isLit := archtest.StringLit(lit)
	require.True(t, isLit)
	require.Equal(t, "x", value)

	call, err := parser.ParseExpr(`tenancy.WithAllTenants(ctx)`)
	require.NoError(t, err)

	_, found := archtest.SelectorCall(call, "tenancy", "WithAllTenants")
	require.True(t, found)

	_, found = archtest.SelectorCall(call, "tenancy", "WithTenantID")
	require.False(t, found)
}
