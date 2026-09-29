package authzts_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/authz/authzts"
)

func small(t *testing.T) *authz.Catalogue {
	c := authz.NewCatalogue()
	require.NoError(t, c.Define("crm.offer:view", authz.Label("perm.crm.offer.view"), authz.Ownable("crm.offer")))
	require.NoError(t, c.Define("crm.offer:update", authz.Requires("crm.offer:view"), authz.Ownable("crm.offer"), authz.Attributes("vehiclekind")))
	require.NoError(t, c.Define("usedvehicle.evaluation.offer:decide", authz.Sensitive(), authz.Feature("auction"), authz.System(), authz.OrganizationOnly()))
	require.NoError(t, c.Define("a.b:run", authz.Description("quote'd")))
	return c
}

func TestRenderUnionAndMetadata(t *testing.T) {
	out := authzts.Render(small(t), authzts.Options{Hierarchy: authztest.Hierarchy()})

	assert.True(t, strings.HasPrefix(out, "// This file is auto-generated. Do not edit manually.\n"))
	assert.Contains(t, out, "export type Permission =\n  | 'a.b:run'\n  | 'crm.offer:update'\n  | 'crm.offer:view'\n  | 'usedvehicle.evaluation.offer:decide';\n",
		"a sorted, exact union")
	assert.Contains(t, out, "export type Qualifier =\n  | 'own'\n  | 'own_location'\n  | 'all';\n")
	assert.Contains(t, out, "export type ScopeLevel =\n  | 'organization'\n  | 'dealer'\n  | 'location';\n")
	assert.Contains(t, out, "export const permissions: Record<Permission, PermissionMeta> = {\n")

	assert.Contains(t, out, "'crm.offer:update': { module: 'crm', resource: 'offer', verb: 'update', labelKey: '', descriptionKey: '', sensitive: false, system: false, organizationOnly: false, ownable: 'crm.offer', unownedIsOwn: false, feature: null, attributes: ['vehiclekind'], requires: ['crm.offer:view'] },")
	assert.Contains(t, out, "'usedvehicle.evaluation.offer:decide': { module: 'usedvehicle', resource: 'evaluation.offer', verb: 'decide', labelKey: '', descriptionKey: '', sensitive: true, system: true, organizationOnly: true, ownable: null, unownedIsOwn: false, feature: 'auction', attributes: [], requires: [] },")
	assert.Contains(t, out, "labelKey: 'perm.crm.offer.view'")
	assert.Contains(t, out, "descriptionKey: 'quote\\'d'")
	assert.Contains(t, out, "export interface MyAccess {")
	assert.Contains(t, out, "level: ScopeLevel;")
}

func TestRenderWithoutHierarchy(t *testing.T) {
	out := authzts.Render(small(t), authzts.Options{})
	assert.NotContains(t, out, "ScopeLevel")
	assert.Contains(t, out, "level: string;")
}

func TestRenderIsDeterministicAndOrderIndependent(t *testing.T) {
	a := authz.NewCatalogue()
	b := authz.NewCatalogue()
	ids := []string{"z.z:view", "a.a:view", "m.m:view", "m.m:update"}
	for _, id := range ids {
		require.NoError(t, a.Define(id))
	}
	for _, id := range slices.Backward(ids) {
		require.NoError(t, b.Define(id))
	}
	assert.Equal(t, authzts.Render(a, authzts.Options{}), authzts.Render(b, authzts.Options{}))
}

func TestRenderEmptyCatalogue(t *testing.T) {
	out := authzts.Render(authz.NewCatalogue(), authzts.Options{})
	assert.Contains(t, out, "export type Permission = never;")
}

func TestWriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "permissions.ts")
	require.NoError(t, authzts.WriteFile(small(t), path, authzts.Options{}))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, authzts.Render(small(t), authzts.Options{}), string(got))
}

func TestDealershipSampleRenders(t *testing.T) {
	out := authzts.Render(authztest.MustDealership().Catalogue, authzts.Options{Hierarchy: authztest.Hierarchy()})
	assert.Equal(t, 162+3+3, strings.Count(out, "\n  | '"), "one union member per permission, plus 3 qualifiers and 3 levels")
}
