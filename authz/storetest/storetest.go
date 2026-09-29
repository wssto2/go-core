// Package storetest is the conformance suite every authz.Store must pass.
//
//	func TestMyStore(t *testing.T) {
//		storetest.Run(t, func(t *testing.T) authz.Store { return newMyStore(t) })
//	}
//
// Each subtest gets a fresh, empty store from the factory.
package storetest

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/wssto2/go-core/authz"
)

// Factory returns a new empty store for one subtest.
type Factory func(t *testing.T) authz.Store

func actor() authz.Subject { return authz.Subject{Kind: authz.KindUser, ID: 99} }
func alice() authz.Subject { return authz.Subject{Kind: authz.KindUser, ID: 1} }
func bob() authz.Subject   { return authz.Subject{Kind: authz.KindUser, ID: 2} }

// robot is a service account with the same ID as alice.
func robot() authz.Subject      { return authz.Subject{Kind: authz.KindServiceAccount, ID: 1} }
func dealer3() authz.Scope      { return authz.Scope{Level: "dealer", ID: 3} }
func location7() authz.Scope    { return authz.Scope{Level: "location", ID: 7} }
func organization() authz.Scope { return authz.Scope{Level: "organization"} }

// check is the small assertion kit the suite uses, so the package needs no test library.
type check struct{ t *testing.T }

func (c check) noErr(err error, what string) {
	c.t.Helper()
	if err != nil {
		c.t.Fatalf("%s: unexpected error: %v", what, err)
	}
}

func (c check) isErr(err, target error, what string) {
	c.t.Helper()
	if !errors.Is(err, target) {
		c.t.Errorf("%s: got error %v, want %v", what, err, target)
	}
}

func (c check) equal(want, got any, what string) {
	c.t.Helper()
	if !reflect.DeepEqual(want, got) {
		c.t.Errorf("%s: got %#v, want %#v", what, got, want)
	}
}

func (c check) true(ok bool, what string) {
	c.t.Helper()
	if !ok {
		c.t.Errorf("%s", what)
	}
}

func sameElements[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	rest := slices.Clone(b)
	for _, x := range a {
		i := slices.Index(rest, x)
		if i < 0 {
			return false
		}
		rest = slices.Delete(rest, i, i+1)
	}
	return true
}

// Run executes the suite.
func Run(t *testing.T, newStore Factory) {
	t.Helper()
	cases := map[string]func(check, authz.Store){
		"RoleRoundTrip":                roleRoundTrip,
		"RoleUpdateReplacesEverything": roleUpdate,
		"RoleNotFound":                 roleNotFound,
		"RolesAreListedByID":           listRoles,
		"BindingRoundTrip":             bindingRoundTrip,
		"BindingsAreKeptPerSubject":    bindingsPerSubject,
		"DuplicateBindings":            duplicateBindings,
		"UnbindAndNotFound":            unbind,
		"DeleteRoleInUse":              deleteRoleInUse,
		"BindingsForRole":              bindingsForRole,
		"ConcurrentBinds":              concurrentBinds,
	}
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		t.Run(name, func(t *testing.T) { cases[name](check{t}, newStore(t)) })
	}
}

func sampleRole() authz.Role {
	return authz.Role{
		Name:        "Sales",
		Description: "sells things",
		Grants: []authz.Grant{
			{Permission: "crm.lead:view", Qualifier: authz.QualifierOwn},
			{Permission: "crm.offer:view", Qualifier: authz.QualifierAll},
			{Permission: "crm.contract:view", Qualifier: authz.QualifierOwnLocation},
		},
		Attrs: map[string][]string{"vehiclekind": {"used", "new"}},
	}
}

func roleRoundTrip(c check, s authz.Store) {
	ctx := context.Background()
	in := sampleRole()
	in.Key = "ignored"        // a stored role has no key
	in.Computed = authz.All() // and cannot be computed
	saved, err := s.SaveRole(ctx, actor(), in)
	c.noErr(err, "save")
	c.true(saved.ID > 0, "a saved role gets an ID")
	c.equal("", saved.Key, "the key is dropped")

	got, err := s.Role(ctx, saved.ID)
	c.noErr(err, "load")
	c.equal(saved.ID, got.ID, "ID")
	c.equal("Sales", got.Name, "name")
	c.equal("sells things", got.Description, "description")
	c.true(sameElements(sampleRole().Grants, got.Grants), "grants round-trip")
	c.true(sameElements([]string{"used", "new"}, got.Attrs["vehiclekind"]), "attributes round-trip")
	c.true(got.Computed == nil, "a stored role is never computed")

	bare, err := s.SaveRole(ctx, actor(), authz.Role{Name: "Bare"}) // no grants, no attributes
	c.noErr(err, "save bare")
	got, err = s.Role(ctx, bare.ID)
	c.noErr(err, "load bare")
	c.true(len(got.Grants) == 0 && len(got.Attrs) == 0, "a bare role stays bare")
}

func roleUpdate(c check, s authz.Store) {
	ctx := context.Background()
	saved, err := s.SaveRole(ctx, actor(), sampleRole())
	c.noErr(err, "save")

	saved.Name = "Renamed"
	saved.Description = ""
	saved.Grants = []authz.Grant{{Permission: "crm.offer:view", Qualifier: authz.QualifierAll}}
	saved.Attrs = nil
	updated, err := s.SaveRole(ctx, actor(), saved)
	c.noErr(err, "update")
	c.equal(saved.ID, updated.ID, "an update keeps the ID")

	got, err := s.Role(ctx, saved.ID)
	c.noErr(err, "load")
	c.equal("Renamed", got.Name, "name")
	c.equal("", got.Description, "description")
	c.equal(saved.Grants, got.Grants, "old grants are gone")
	c.true(len(got.Attrs) == 0, "old attributes are gone")

	roles, err := s.ListRoles(ctx)
	c.noErr(err, "list")
	c.equal(1, len(roles), "an update does not create a second role")
}

func roleNotFound(c check, s authz.Store) {
	ctx := context.Background()
	_, err := s.Role(ctx, 12345)
	c.isErr(err, authz.ErrRoleNotFound, "Role")
	_, err = s.SaveRole(ctx, actor(), authz.Role{ID: 12345, Name: "ghost"})
	c.isErr(err, authz.ErrRoleNotFound, "SaveRole of an unknown ID")
	c.isErr(s.DeleteRole(ctx, actor(), 12345), authz.ErrRoleNotFound, "DeleteRole")
}

func listRoles(c check, s authz.Store) {
	ctx := context.Background()
	for _, name := range []string{"B", "A", "C"} {
		_, err := s.SaveRole(ctx, actor(), authz.Role{Name: name, Grants: authz.Grants(authz.QualifierAll, "vehicle.stock:view")})
		c.noErr(err, "save "+name)
	}
	roles, err := s.ListRoles(ctx)
	c.noErr(err, "list")
	if len(roles) != 3 {
		c.t.Fatalf("got %d roles, want 3", len(roles))
	}
	c.equal([]string{"B", "A", "C"}, []string{roles[0].Name, roles[1].Name, roles[2].Name}, "ordered by ID")
	c.true(roles[0].ID < roles[1].ID, "IDs ascend")
	c.equal(1, len(roles[0].Grants), "listed roles carry their grants")
}

func bindingRoundTrip(c check, s authz.Store) {
	ctx := context.Background()
	role, err := s.SaveRole(ctx, actor(), sampleRole())
	c.noErr(err, "save role")

	byID, err := s.Bind(ctx, actor(), authz.Binding{Subject: alice(), Role: authz.RoleRef{ID: role.ID}, Scope: location7()})
	c.noErr(err, "bind by ID")
	c.true(byID.ID > 0, "a binding gets an ID")
	c.equal(actor().ID, byID.CreatedBy, "created by")
	c.true(!byID.CreatedAt.IsZero(), "created at")

	byKey, err := s.Bind(ctx, actor(), authz.Binding{Subject: alice(), Role: authz.RoleRef{Key: "webmaster"}, Scope: organization()})
	c.noErr(err, "bind by key")
	c.true(byID.ID != byKey.ID, "distinct IDs")

	got, err := s.Binding(ctx, byID.ID)
	c.noErr(err, "load")
	c.equal(alice(), got.Subject, "subject")
	c.equal(authz.RoleRef{ID: role.ID}, got.Role, "role")
	c.equal(location7(), got.Scope, "scope")

	got, err = s.Binding(ctx, byKey.ID)
	c.noErr(err, "load by key")
	c.equal(authz.RoleRef{Key: "webmaster"}, got.Role, "role key")
	c.equal(organization(), got.Scope, "the root scope has no ID")
}

func bindingsPerSubject(c check, s authz.Store) {
	ctx := context.Background()
	for _, b := range []authz.Binding{
		{Subject: alice(), Role: authz.RoleRef{Key: "seller"}, Scope: dealer3()},
		{Subject: alice(), Role: authz.RoleRef{Key: "seller"}, Scope: location7()},
		{Subject: bob(), Role: authz.RoleRef{Key: "seller"}, Scope: dealer3()},
		{Subject: robot(), Role: authz.RoleRef{Key: "seller"}, Scope: dealer3()},
	} {
		_, err := s.Bind(ctx, actor(), b)
		c.noErr(err, "bind")
	}
	got, err := s.BindingsFor(ctx, alice())
	c.noErr(err, "alice")
	if len(got) != 2 {
		c.t.Fatalf("alice has %d bindings, want 2", len(got))
	}
	c.equal(dealer3(), got[0].Scope, "ordered by ID")
	c.true(got[0].Subject == alice() && got[1].Subject == alice(), "only alice's bindings")

	got, err = s.BindingsFor(ctx, robot())
	c.noErr(err, "robot")
	c.equal(1, len(got), "a service account is not the user with the same ID")

	got, err = s.BindingsFor(ctx, authz.Subject{Kind: authz.KindUser, ID: 404})
	c.noErr(err, "stranger")
	c.equal(0, len(got), "an unknown subject has no bindings, and that is not an error")
}

func duplicateBindings(c check, s authz.Store) {
	ctx := context.Background()
	b := authz.Binding{Subject: alice(), Role: authz.RoleRef{Key: "seller"}, Scope: dealer3()}
	_, err := s.Bind(ctx, actor(), b)
	c.noErr(err, "first")
	_, err = s.Bind(ctx, actor(), b)
	c.isErr(err, authz.ErrDuplicateBinding, "identical binding")

	for _, other := range []authz.Binding{ // a different role, scope or subject is no duplicate
		{Subject: alice(), Role: authz.RoleRef{Key: "manager"}, Scope: dealer3()},
		{Subject: alice(), Role: authz.RoleRef{Key: "seller"}, Scope: location7()},
		{Subject: bob(), Role: authz.RoleRef{Key: "seller"}, Scope: dealer3()},
	} {
		_, err := s.Bind(ctx, actor(), other)
		c.noErr(err, "distinct binding")
	}
	// the root has no ID, so it is at most once per role too
	_, err = s.Bind(ctx, actor(), authz.Binding{Subject: alice(), Role: authz.RoleRef{Key: "webmaster"}, Scope: organization()})
	c.noErr(err, "root")
	_, err = s.Bind(ctx, actor(), authz.Binding{Subject: alice(), Role: authz.RoleRef{Key: "webmaster"}, Scope: organization()})
	c.isErr(err, authz.ErrDuplicateBinding, "root twice")
}

func unbind(c check, s authz.Store) {
	ctx := context.Background()
	b, err := s.Bind(ctx, actor(), authz.Binding{Subject: alice(), Role: authz.RoleRef{Key: "seller"}, Scope: dealer3()})
	c.noErr(err, "bind")
	c.noErr(s.Unbind(ctx, actor(), b.ID), "unbind")
	_, err = s.Binding(ctx, b.ID)
	c.isErr(err, authz.ErrBindingNotFound, "load after unbind")
	c.isErr(s.Unbind(ctx, actor(), b.ID), authz.ErrBindingNotFound, "unbind twice")
	got, err := s.BindingsFor(ctx, alice())
	c.noErr(err, "list")
	c.equal(0, len(got), "nothing left")

	_, err = s.Bind(ctx, actor(), authz.Binding{Subject: alice(), Role: authz.RoleRef{Key: "seller"}, Scope: dealer3()})
	c.noErr(err, "the same binding can be made again")
}

func deleteRoleInUse(c check, s authz.Store) {
	ctx := context.Background()
	role, err := s.SaveRole(ctx, actor(), sampleRole())
	c.noErr(err, "save role")
	b, err := s.Bind(ctx, actor(), authz.Binding{Subject: alice(), Role: authz.RoleRef{ID: role.ID}, Scope: dealer3()})
	c.noErr(err, "bind")

	c.isErr(s.DeleteRole(ctx, actor(), role.ID), authz.ErrRoleInUse, "delete while bound")
	_, err = s.Role(ctx, role.ID)
	c.noErr(err, "a refused delete removes nothing")

	c.noErr(s.Unbind(ctx, actor(), b.ID), "unbind")
	c.noErr(s.DeleteRole(ctx, actor(), role.ID), "delete")
	_, err = s.Role(ctx, role.ID)
	c.isErr(err, authz.ErrRoleNotFound, "load after delete")
}

func bindingsForRole(c check, s authz.Store) {
	ctx := context.Background()
	r1, err := s.SaveRole(ctx, actor(), sampleRole())
	c.noErr(err, "role 1")
	r2, err := s.SaveRole(ctx, actor(), sampleRole())
	c.noErr(err, "role 2")
	for _, b := range []authz.Binding{
		{Subject: alice(), Role: authz.RoleRef{ID: r1.ID}, Scope: dealer3()},
		{Subject: bob(), Role: authz.RoleRef{ID: r1.ID}, Scope: location7()},
		{Subject: alice(), Role: authz.RoleRef{ID: r2.ID}, Scope: dealer3()},
		{Subject: alice(), Role: authz.RoleRef{Key: "seller"}, Scope: dealer3()},
	} {
		_, err := s.Bind(ctx, actor(), b)
		c.noErr(err, "bind")
	}
	got, err := s.BindingsForRole(ctx, r1.ID)
	c.noErr(err, "list")
	if len(got) != 2 {
		c.t.Fatalf("role 1 has %d bindings, want 2", len(got))
	}
	c.equal([]authz.Subject{alice(), bob()}, []authz.Subject{got[0].Subject, got[1].Subject}, "the holders, in binding order")
}

func concurrentBinds(c check, s authz.Store) {
	ctx := context.Background()
	const n = 10

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n { // the very same binding, n times at once: exactly one wins
		wg.Go(func() {
			_, errs[i] = s.Bind(ctx, actor(), authz.Binding{Subject: alice(), Role: authz.RoleRef{Key: "seller"}, Scope: dealer3()})
		})
	}
	wg.Wait()
	won, dup := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, authz.ErrDuplicateBinding):
			dup++
		default:
			c.t.Errorf("unexpected error: %v", err)
		}
	}
	c.equal(1, won, "exactly one identical Bind wins")
	c.equal(n-1, dup, "the others are duplicates")

	for i := range n { // n different bindings at once: all succeed
		wg.Go(func() {
			_, errs[i] = s.Bind(ctx, actor(), authz.Binding{Subject: bob(), Role: authz.RoleRef{Key: "seller"}, Scope: authz.Scope{Level: "location", ID: 100 + i}})
		})
	}
	wg.Wait()
	for _, err := range errs {
		c.noErr(err, "concurrent distinct bind")
	}
	got, err := s.BindingsFor(ctx, bob())
	c.noErr(err, "list")
	c.equal(n, len(got), "every distinct binding was stored")
}
