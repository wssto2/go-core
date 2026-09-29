package authztest

import (
	"context"
	"sync"

	"github.com/wssto2/go-core/authz"
)

// Fake is an authz.Authorizer for unit tests of services: no database, no
// bindings. Allow the permissions the code under test needs; everything else is
// denied. Record what was checked with Checked.
type Fake struct {
	mu      sync.Mutex
	allowed map[string]bool
	all     bool
	access  map[string][]authz.Clause
	checked []string
}

var _ authz.Authorizer = (*Fake)(nil)

// AllowAll returns a Fake that permits every check, over every record.
func AllowAll() *Fake { return &Fake{all: true, allowed: map[string]bool{}} }

// DenyAll returns a Fake that permits nothing.
func DenyAll() *Fake { return &Fake{allowed: map[string]bool{}} }

// Allow permits the permissions (Require, RequireOn) and gives them an
// unrestricted AccessSet unless WithClauses says otherwise.
func (f *Fake) Allow(permissions ...string) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range permissions {
		f.allowed[p] = true
	}
	return f
}

// WithClauses sets the clauses Access returns for the permission and allows it.
// RequireOn still passes for any resource: test record-level rules with a World.
func (f *Fake) WithClauses(permission string, clauses ...authz.Clause) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.access == nil {
		f.access = map[string][]authz.Clause{}
	}
	f.access[permission] = clauses
	f.allowed[permission] = true
	return f
}

// Checked returns the permissions checked so far, in order.
func (f *Fake) Checked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.checked...)
}

func (f *Fake) check(permission string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked = append(f.checked, permission)
	return f.all || f.allowed[permission]
}

// Require implements authz.Authorizer.
func (f *Fake) Require(_ context.Context, permission string) error {
	if !f.check(permission) {
		return denied(permission)
	}
	return nil
}

// RequireOn implements authz.Authorizer.
func (f *Fake) RequireOn(ctx context.Context, permission string, _ authz.Resource) error {
	return f.Require(ctx, permission)
}

// Access implements authz.Authorizer. An allowed permission without explicit
// clauses gets one unrestricted clause at the "organization" root.
func (f *Fake) Access(ctx context.Context, permission string) (authz.AccessSet, error) {
	p, _ := authz.PrincipalFrom(ctx)
	set := authz.AccessSet{Permission: permission, Principal: p}
	if !f.check(permission) {
		return set, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if clauses, ok := f.access[permission]; ok {
		set.Clauses = append([]authz.Clause(nil), clauses...)
		return set, nil
	}
	set.Clauses = []authz.Clause{{
		Scope: Org(), Chain: authz.Chain{Org()}, Qualifier: authz.QualifierAll,
	}}
	return set, nil
}

func denied(permission string) error {
	return authz.Deny(permission)
}
