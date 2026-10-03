package admin_test

import (
	"fmt"

	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

func exampleFixture() (*fixture, func()) {
	f, cleanup, err := buildFixture()
	if err != nil {
		panic(err)
	}

	f.t = nil // examples seed through the store directly

	return f, cleanup
}

func seed(f *fixture, id int, role string, scope authz.Scope) {
	if _, err := f.Store.Bind(as(1), authz.Subject{}, authz.Binding{Subject: user(id), Role: authz.RoleRef{Key: role}, Scope: scope}); err != nil {
		panic(err)
	}

	f.Engine.Evict(user(id))
}

// New builds the role and binding services around one engine, one store, one
// directory of who can hold a role and one catalogue of the places.
func ExampleNew() {
	f, cleanup := exampleFixture()
	defer cleanup()

	roles, bindings, err := admin.New(admin.Config{
		Engine: f.Engine, Store: f.Store, Scopes: admin.NoScopes(), Subjects: newDirectory(),
		Permissions: admin.DefaultPermissions, Transactor: noTx{},
	})
	fmt.Println(roles != nil, bindings != nil, err)
	// Output: true true <nil>
}

func ExampleRoles_Create() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 1, "webmaster", authztest.Org())

	role, err := f.Roles.Create(as(1), admin.RoleDraft{
		Name:   "Clerk",
		Grants: authz.Grants(authz.QualifierAll, "crm.customer:view"),
	})
	fmt.Println(role.Ref, role.Name, role.PermissionCount, err)

	// nobody builds a role that grants more than they hold
	seed(f, 2, "roleadmin", authztest.Org())
	_, err = f.Roles.Create(as(2), admin.RoleDraft{Name: "Too much", Grants: authz.Grants(authz.QualifierAll, "system.job:run")})
	fmt.Println(err)
	// Output:
	// 1 Clerk 1 <nil>
	// cannot grant more than you hold: authz: escalation: system.job:run: not held
}

func ExampleRoles_List() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 1, "webmaster", authztest.Org())
	seed(f, 2, "seller", authztest.Dealer(10))

	list, _ := f.Roles.List(as(1))
	for _, role := range list[:2] {
		fmt.Println(role.Ref, role.Holders)
	}
	// Output:
	// webmaster 1
	// dealeradmin 0
}

func ExampleRoles_Holders() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 1, "webmaster", authztest.Org())
	seed(f, 2, "seller", authztest.Location(101))

	holders, _ := f.Roles.Holders(as(1), "seller")
	for _, h := range holders {
		fmt.Println(h.Name, "at", h.Place.Name)
	}
	// Output: Boris at North main street
}

func ExampleRoles_Replace() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 1, "webmaster", authztest.Org())

	clerk, _ := f.Roles.Create(as(1), admin.RoleDraft{Name: "Clerk", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")})
	f.seedCustomExample(5, clerk.ID, authztest.Dealer(10))

	rebound, err := f.Roles.Replace(as(1), clerk.Ref, "seller")
	fmt.Println(rebound, err)
	// Output: 1 <nil>
}

func ExampleRoles_Compare() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 1, "webmaster", authztest.Org())

	custom, _ := f.Roles.Create(as(1), admin.RoleDraft{Name: "Mixed", Grants: authz.Grants(authz.QualifierAll, "crm.lead:view", "crm.customer:view")})
	got, _ := f.Roles.Compare(as(1), custom.Ref, "seller")
	for _, d := range got.Different {
		fmt.Println(d.Permission, d.Role, "instead of", d.Other)
	}
	// Output: crm.lead:view all instead of own
}

func ExampleRoles_Show() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 1, "webmaster", authztest.Org())

	role, _ := f.Roles.Show(as(1), "seller")
	for _, g := range role.Grants {
		fmt.Println(g.Permission, g.Qualifier)
	}
	// Output:
	// crm.customer:view all
	// crm.lead:view own
}

func ExampleRoles_Update() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 1, "webmaster", authztest.Org())

	role, _ := f.Roles.Create(as(1), admin.RoleDraft{Name: "Clerk", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")})
	role, _ = f.Roles.Update(as(1), role.Ref, admin.RoleDraft{Name: "Senior clerk", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view", "crm.lead:view")})
	fmt.Println(role.Name, role.PermissionCount)

	_, err := f.Roles.Update(as(1), "seller", admin.RoleDraft{Name: "x"}) // predefined roles live in code
	fmt.Println(err)
	// Output:
	// Senior clerk 2
	// access: a predefined role is defined in code and cannot be changed: access: a predefined role is defined in code and cannot be changed
}

func ExampleRoles_Delete() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 1, "webmaster", authztest.Org())

	role, _ := f.Roles.Create(as(1), admin.RoleDraft{Name: "Clerk", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")})
	fmt.Println(f.Roles.Delete(as(1), role.Ref))
	// Output: <nil>
}

func ExampleBindings_Bind() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 2, "dealeradmin", authztest.Dealer(10))

	b, err := f.Bindings.Bind(as(2), user(5), admin.BindingDraft{Role: "seller", Scope: authztest.Location(101)})
	fmt.Println(b.Role.Name, "at", b.Place.Name, err)

	// a dealer administrator cannot give a role at another dealer, or to themselves
	_, err = f.Bindings.Bind(as(2), user(5), admin.BindingDraft{Role: "seller", Scope: authztest.Dealer(20)})
	fmt.Println(err)
	_, err = f.Bindings.Bind(as(2), user(2), admin.BindingDraft{Role: "seller", Scope: authztest.Dealer(10)})
	fmt.Println(err)
	// Output:
	// Seller at North main street <nil>
	// access denied: authz: forbidden
	// you cannot assign a role to yourself: authz: cannot assign a role to yourself
}

func ExampleBindings_Unbind() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 2, "dealeradmin", authztest.Dealer(10))
	b, _ := f.Bindings.Bind(as(2), user(5), admin.BindingDraft{Role: "seller", Scope: authztest.Dealer(10)})
	fmt.Println(f.Bindings.Unbind(as(2), user(5), b.ID))
	// Output: <nil>
}

func ExampleBindings_Access() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 1, "webmaster", authztest.Org())
	seed(f, 5, "seller", authztest.Dealer(10))

	access, _ := f.Bindings.Access(as(1), user(5))
	for _, p := range access.Effective {
		fmt.Println(p.Permission, p.Grants[0].RoleName, "at", p.Grants[0].Place.Name)
	}
	// Output:
	// crm.customer:view Seller at North
	// crm.lead:view Seller at North
}

func ExampleBindings_Scopes() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 2, "dealeradmin", authztest.Dealer(10))

	options, _ := f.Bindings.Scopes(as(2), user(5))
	fmt.Println(options.Root)

	for _, p := range options.Places {
		fmt.Println(p.Scope, p.Name)
	}
	// Output:
	// false
	// dealer:10 North
	// location:101 North main street
}

func ExampleBindings_BindableRoles() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 2, "dealeradmin", authztest.Dealer(10))

	roles, _ := f.Bindings.BindableRoles(as(2), authztest.Dealer(10))
	for _, r := range roles {
		fmt.Println(r.Ref)
	}
	// Output:
	// dealeradmin
	// roleadmin
	// salesmanager
	// seller
}

func ExampleBindings_Mine() {
	f, cleanup := exampleFixture()
	defer cleanup()

	seed(f, 5, "seller", authztest.Dealer(10))

	mine, _ := f.Bindings.Mine(as(5))
	fmt.Println(mine[0].Role.Name, "at", mine[0].Place.Name)
	// Output: Seller at North
}

func ExampleNoScopes() {
	scopes := admin.NoScopes()
	options, _ := scopes.Options(as(1))
	fmt.Println(len(options), "places below the organization")
	// Output: 0 places below the organization
}

func ExamplePermissions() {
	custom := admin.DefaultPermissions
	custom.ManageBindings = "team.member:manage" // the application's own id

	fmt.Println(admin.DefaultPermissions.ViewRoles, custom.ManageBindings)
	// Output: iam.role:view team.member:manage
}
