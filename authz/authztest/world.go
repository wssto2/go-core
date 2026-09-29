package authztest

import (
	"context"
	"testing"

	"github.com/wssto2/go-core/authz"
)

// World is a real Engine (and Admin) over in-memory parts, for tests of code
// that calls authz. Build it with NewWorld, add places, bind people, then act
// as them with As.
type World struct {
	tb        testing.TB
	Catalogue *authz.Catalogue
	Hierarchy *authz.Hierarchy
	Store     *MemoryStore
	Places    *Places
	Features  *Features
	Engine    *authz.Engine
}

// NewWorld builds an engine over the catalogue with the predefined roles, the
// Hierarchy() levels, a MemoryStore, Places and Features. It fails the test on
// any configuration error.
func NewWorld(tb testing.TB, cat *authz.Catalogue, roles ...authz.Role) *World {
	tb.Helper()
	w := &World{
		tb: tb, Catalogue: cat, Hierarchy: Hierarchy(), Store: NewMemoryStore(),
		Places: NewPlaces(), Features: NewFeatures(),
	}
	engine, err := authz.NewEngine(authz.Config{
		Catalogue: cat, Hierarchy: w.Hierarchy, Roles: roles, Store: w.Store,
		Resolver: w.Places, Features: w.Features,
	})
	if err != nil {
		tb.Fatalf("authztest: build engine: %v", err)
	}
	w.Engine = engine
	return w
}

// Bind gives the subject a predefined role at a scope, straight into the store
// (no delegation checks), and evicts the subject's cached access.
func (w *World) Bind(s authz.Subject, roleKey string, scope authz.Scope) authz.Binding {
	w.tb.Helper()
	b, err := w.Store.Bind(context.Background(), authz.Subject{}, authz.Binding{
		Subject: s, Role: authz.RoleRef{Key: roleKey}, Scope: scope,
	})
	if err != nil {
		w.tb.Fatalf("authztest: bind %s to %s at %s: %v", s, roleKey, scope, err)
	}
	w.Engine.Evict(s)
	return b
}

// BindCustom is Bind for a stored role.
func (w *World) BindCustom(s authz.Subject, roleID int, scope authz.Scope) authz.Binding {
	w.tb.Helper()
	b, err := w.Store.Bind(context.Background(), authz.Subject{}, authz.Binding{
		Subject: s, Role: authz.RoleRef{ID: roleID}, Scope: scope,
	})
	if err != nil {
		w.tb.Fatalf("authztest: bind %s to role %d at %s: %v", s, roleID, scope, err)
	}
	w.Engine.Evict(s)
	return b
}

// SaveRole stores a custom role directly (no delegation checks).
func (w *World) SaveRole(r authz.Role) authz.Role {
	w.tb.Helper()
	saved, err := w.Store.SaveRole(context.Background(), authz.Subject{}, r)
	if err != nil {
		w.tb.Fatalf("authztest: save role: %v", err)
	}
	return saved
}

// Admin returns an Admin with the given management permissions.
func (w *World) Admin(manageRoles, manageBindings string, protected ...string) *authz.Admin {
	w.tb.Helper()
	a, err := authz.NewAdmin(authz.AdminConfig{
		Engine: w.Engine, Store: w.Store, ManageRoles: manageRoles,
		ManageBindings: manageBindings, Protected: protected,
	})
	if err != nil {
		w.tb.Fatalf("authztest: build admin: %v", err)
	}
	return a
}

// As returns a context acting as the principal.
func (w *World) As(p authz.Principal) context.Context {
	return authz.WithPrincipal(context.Background(), p)
}
