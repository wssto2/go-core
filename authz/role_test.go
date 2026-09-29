package authz_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
)

func problems(t *testing.T, err error) *authz.ValidationError {
	t.Helper()
	var ve *authz.ValidationError
	require.ErrorAs(t, err, &ve)
	return ve
}

func TestRoleValidate(t *testing.T) {
	cat := smallCatalogue(t)
	all, own, loc := authz.QualifierAll, authz.QualifierOwn, authz.QualifierOwnLocation
	tests := []struct {
		name string
		role authz.Role
		want authz.ProblemCode // empty: valid
	}{
		{"valid", authz.Role{Name: "r", Grants: []authz.Grant{{"crm.lead:view", own}, {"crm.lead:update", own}}}, ""},
		{"unknown permission", authz.Role{Name: "r", Grants: []authz.Grant{{"crm.nope:view", all}}}, authz.ProblemUnknownPermission},
		{"qualifier on non-ownable", authz.Role{Name: "r", Grants: []authz.Grant{{"vehicle.stock:view", own}}}, authz.ProblemQualifierNotAllowed},
		{"own location on non-ownable", authz.Role{Name: "r", Grants: []authz.Grant{{"vehicle.stock:view", loc}}}, authz.ProblemQualifierNotAllowed},
		{"all on non-ownable is fine", authz.Role{Name: "r", Grants: []authz.Grant{{"vehicle.stock:view", all}}}, ""},
		{"zero qualifier", authz.Role{Name: "r", Grants: []authz.Grant{{"crm.lead:view", 0}}}, authz.ProblemInvalidQualifier},
		{"missing required", authz.Role{Name: "r", Grants: []authz.Grant{{"crm.lead:update", own}}}, authz.ProblemMissingRequired},
		{"missing required non-ownable", authz.Role{Name: "r", Grants: []authz.Grant{{"crm.lead:assign", all}}}, authz.ProblemMissingRequired},
		{"required narrower than dependent", authz.Role{Name: "r", Grants: []authz.Grant{{"crm.lead:view", own}, {"crm.lead:update", all}}}, authz.ProblemRequiredTooNarrow},
		{"required wider than dependent is fine", authz.Role{Name: "r", Grants: []authz.Grant{{"crm.lead:view", all}, {"crm.lead:update", own}}}, ""},
		{"own location above own", authz.Role{Name: "r", Grants: []authz.Grant{{"crm.lead:view", own}, {"crm.lead:update", loc}}}, authz.ProblemRequiredTooNarrow},
		{"duplicate grant", authz.Role{Name: "r", Grants: []authz.Grant{{"crm.lead:view", own}, {"crm.lead:view", all}}}, authz.ProblemDuplicateGrant},
		{"unknown attribute", authz.Role{Name: "r", Attrs: map[string][]string{"colour": {"red"}}}, authz.ProblemUnknownAttribute},
		{"empty attribute list", authz.Role{Name: "r", Attrs: map[string][]string{"vehiclekind": {}}}, authz.ProblemEmptyAttribute},
		{"no name", authz.Role{Grants: []authz.Grant{{"vehicle.stock:view", all}}}, authz.ProblemInvalidRole},
		{"bad key", authz.Role{Key: "Bad Key", Name: "r"}, authz.ProblemInvalidRole},
		{"computed with grants", authz.Role{Name: "r", Computed: authz.All(), Grants: []authz.Grant{{"vehicle.stock:view", all}}}, authz.ProblemComputedWithGrants},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.role.Validate(cat)
			if tt.want == "" {
				require.NoError(t, err)
				return
			}
			assert.True(t, problems(t, err).Has(tt.want), "want %s, got %v", tt.want, err)
		})
	}
}

func TestRoleValidateReportsAllProblemsAtOnce(t *testing.T) {
	role := authz.Role{Name: "r", Grants: []authz.Grant{{"nope:view", authz.QualifierAll}, {"vehicle.stock:view", authz.QualifierOwn}}}
	ve := problems(t, role.Validate(smallCatalogue(t)))
	assert.Len(t, ve.Problems, 2)
}

func TestComputedRolesPickUpNewPermissions(t *testing.T) {
	cat := smallCatalogue(t)
	webmaster := authz.ComputedRole("webmaster", "Webmaster", authz.All())
	importer := authz.ComputedRole("importer", "Importer", authz.AllExcept(authz.IsSystem))
	readonly := authz.ComputedRole("audit", "Audit", authz.Both(authz.Matching(authz.IsView), authz.AllExcept(authz.IsSystem, authz.IsOrganizationOnly)))

	ids := func(r authz.Role) []string {
		var out []string
		for _, g := range r.Resolve(cat) {
			assert.Equal(t, authz.QualifierAll, g.Qualifier)
			out = append(out, g.Permission)
		}
		return out
	}
	assert.Len(t, ids(webmaster), cat.Len())
	assert.Contains(t, ids(webmaster), "system.job:run")
	assert.NotContains(t, ids(importer), "system.job:run")
	assert.Contains(t, ids(importer), "report.group:view", "organization-only is not system")
	assert.ElementsMatch(t, []string{"crm.lead:view", "crm.offer:view", "vehicle.stock:view", "auction.lot:view"}, ids(readonly))

	// A permission added later is included with no edit to the role.
	before := len(ids(importer))
	require.NoError(t, cat.Define("new.feature:view"))
	require.NoError(t, cat.Define("new.feature:run", authz.System()))
	assert.Len(t, ids(importer), before+1)
	assert.Contains(t, ids(webmaster), "new.feature:run")
	assert.Contains(t, ids(readonly), "new.feature:view")

	for _, r := range []authz.Role{webmaster, importer, readonly} {
		assert.NoError(t, r.Validate(cat), r.Key)
	}
}

func TestComputedRoleMustSatisfyRequires(t *testing.T) {
	cat := smallCatalogue(t)
	// Selecting a permission without the one it requires is caught for computed roles too.
	only := authz.ComputedRole("odd", "Odd", authz.Matching(func(p authz.Permission) bool { return p.ID == "crm.lead:update" }))
	assert.True(t, problems(t, only.Validate(cat)).Has(authz.ProblemMissingRequired))
}

func TestQualifierOrderAndText(t *testing.T) {
	assert.True(t, authz.QualifierAll.Covers(authz.QualifierOwnLocation))
	assert.True(t, authz.QualifierOwnLocation.Covers(authz.QualifierOwn))
	assert.False(t, authz.QualifierOwn.Covers(authz.QualifierOwnLocation))
	assert.False(t, authz.Qualifier(0).Valid())
	for _, q := range []authz.Qualifier{authz.QualifierOwn, authz.QualifierOwnLocation, authz.QualifierAll} {
		b, err := q.MarshalText()
		require.NoError(t, err)
		var back authz.Qualifier
		require.NoError(t, back.UnmarshalText(b))
		assert.Equal(t, q, back)
	}
	_, err := authz.Qualifier(9).MarshalText()
	assert.Error(t, err)
	assert.Error(t, new(authz.Qualifier).UnmarshalText([]byte("everything")))
}

func TestNewEngineRejectsBadPredefinedRoles(t *testing.T) {
	base := func(roles ...authz.Role) error {
		w := authztestParts(t)
		_, err := authz.NewEngine(authz.Config{Catalogue: smallCatalogue(t), Hierarchy: w.h, Roles: roles, Store: w.store, Resolver: w.places, Features: w.features})
		return err
	}
	assert.NoError(t, base(smallRoles()...))
	assert.ErrorContains(t, base(authz.Role{Name: "no key"}), "needs a Key")
	assert.ErrorContains(t, base(authz.Role{Key: "x", Name: "x"}, authz.Role{Key: "x", Name: "x"}), "twice")
	assert.ErrorContains(t, base(authz.Role{Key: "x", Name: "x", Grants: []authz.Grant{{"crm.lead:update", authz.QualifierOwn}}}), "missing_required")
}
