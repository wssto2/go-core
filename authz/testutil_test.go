package authz_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

// smallCatalogue is a compact catalogue that exercises every metadata option.
func smallCatalogue(tb testing.TB) *authz.Catalogue {
	tb.Helper()
	c := authz.NewCatalogue()
	must := func(id string, opts ...authz.DefineOption) {
		tb.Helper()
		require.NoError(tb, c.Define(id, opts...))
	}
	must("crm.lead:view", authz.Ownable("crm.lead"), authz.UnownedIsOwn(), authz.Attributes("vehiclekind"))
	must("crm.lead:update", authz.Ownable("crm.lead"), authz.Attributes("vehiclekind"), authz.Requires("crm.lead:view"))
	must("crm.lead:assign", authz.Requires("crm.lead:view"))
	must("crm.offer:view", authz.Ownable("crm.offer"))
	must("crm.offer:update", authz.Ownable("crm.offer"), authz.Requires("crm.offer:view"))
	must("crm.offer:delete", authz.Ownable("crm.offer"), authz.Sensitive(), authz.Requires("crm.offer:view"))
	must("vehicle.stock:view")
	must("iam.role:manage", authz.Sensitive())
	must("iam.user:manage", authz.Sensitive())
	must("system.job:run", authz.System(), authz.Sensitive())
	must("report.group:view", authz.OrganizationOnly())
	must("auction.lot:view", authz.Feature("auction"))
	return c
}

func smallRoles() []authz.Role {
	return []authz.Role{
		{Key: "seller", Name: "Seller", Grants: []authz.Grant{
			{Permission: "crm.lead:view", Qualifier: authz.QualifierOwn},
			{Permission: "crm.lead:update", Qualifier: authz.QualifierOwn},
			{Permission: "crm.offer:view", Qualifier: authz.QualifierAll},
			{Permission: "crm.offer:update", Qualifier: authz.QualifierOwn},
			{Permission: "vehicle.stock:view", Qualifier: authz.QualifierAll},
			{Permission: "auction.lot:view", Qualifier: authz.QualifierAll},
		}},
		{Key: "usedseller", Name: "Used seller", Attrs: map[string][]string{"vehiclekind": {"used"}}, Grants: authz.Grants(authz.QualifierAll,
			"crm.lead:view", "crm.lead:update")},
		{Key: "manager", Name: "Manager", Grants: []authz.Grant{
			{Permission: "crm.lead:view", Qualifier: authz.QualifierOwnLocation},
			{Permission: "crm.lead:update", Qualifier: authz.QualifierOwnLocation},
			{Permission: "crm.lead:assign", Qualifier: authz.QualifierAll},
			{Permission: "crm.offer:view", Qualifier: authz.QualifierAll},
			{Permission: "crm.offer:update", Qualifier: authz.QualifierAll},
			{Permission: "crm.offer:delete", Qualifier: authz.QualifierAll},
			{Permission: "iam.user:manage", Qualifier: authz.QualifierAll},
		}},
		{Key: "dealerlead", Name: "Dealer lead", Grants: authz.Grants(authz.QualifierAll,
			"crm.lead:view", "crm.lead:update", "crm.lead:assign")},
		authz.ComputedRole("webmaster", "Webmaster", authz.All()),
		authz.ComputedRole("importer", "Importer", authz.AllExcept(authz.IsSystem)),
	}
}

// newSmallWorld returns a world with dealers 1 and 2, locations 10, 11 (dealer 1)
// and 20 (dealer 2), and the auction feature on for dealer 1 only.
func newSmallWorld(tb testing.TB) *authztest.World {
	tb.Helper()
	w := authztest.NewWorld(tb, smallCatalogue(tb), smallRoles()...)
	w.Places.AddDealer(1).AddDealer(2).AddLocation(10, 1).AddLocation(11, 1).AddLocation(20, 2)
	w.Features.Set(authztest.Dealer(1), "auction", true)
	return w
}

func user(id int) authz.Subject { return authz.Subject{Kind: authz.KindUser, ID: id} }

type parts struct {
	h        *authz.Hierarchy
	store    *authztest.MemoryStore
	places   *authztest.Places
	features *authztest.Features
}

func authztestParts(tb testing.TB) parts {
	tb.Helper()
	return parts{authztest.Hierarchy(), authztest.NewMemoryStore(), authztest.NewPlaces(), authztest.NewFeatures()}
}
