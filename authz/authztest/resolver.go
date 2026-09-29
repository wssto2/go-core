package authztest

import (
	"context"
	"fmt"
	"sync"

	"github.com/wssto2/go-core/authz"
)

// Levels are the levels the test helpers use: organization > dealer > location,
// with dealer as the tenant level.
const (
	LevelOrganization = "organization"
	LevelDealer       = "dealer"
	LevelLocation     = "location"
)

// Hierarchy returns organization > dealer > location with dealer as tenant.
func Hierarchy() *authz.Hierarchy {
	h, err := authz.NewHierarchy(LevelOrganization, LevelDealer, LevelLocation)
	if err != nil {
		panic(err) // constant, valid input
	}
	h, err = h.WithTenantLevel(LevelDealer)
	if err != nil {
		panic(err) // constant, valid input
	}
	return h
}

// Org returns the root scope.
func Org() authz.Scope { return authz.Scope{Level: LevelOrganization} }

// Dealer returns the scope of a dealer.
func Dealer(id int) authz.Scope { return authz.Scope{Level: LevelDealer, ID: id} }

// Location returns the scope of a location.
func Location(id int) authz.Scope { return authz.Scope{Level: LevelLocation, ID: id} }

// Places is a ScopeResolver over a map: it knows which dealers and locations
// exist and which dealer a location belongs to. It is safe for concurrent use.
type Places struct {
	mu      sync.RWMutex
	parents map[authz.Scope]authz.Scope
}

// NewPlaces returns an empty set of places.
func NewPlaces() *Places { return &Places{parents: map[authz.Scope]authz.Scope{}} }

// AddDealer registers a dealer under the organization.
func (p *Places) AddDealer(id int) *Places {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.parents[Dealer(id)] = Org()
	return p
}

// AddLocation registers a location of a dealer.
func (p *Places) AddLocation(id, dealer int) *Places {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.parents[Location(id)] = Dealer(dealer)
	return p
}

// Parent implements authz.ScopeResolver.
func (p *Places) Parent(_ context.Context, s authz.Scope) (authz.Scope, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	parent, ok := p.parents[s]
	if !ok {
		return authz.Scope{}, fmt.Errorf("%w: %s", authz.ErrScopeNotFound, s)
	}
	return parent, nil
}

// Features is a FeatureResolver over a set: which tenants have which features.
type Features struct {
	mu sync.RWMutex
	on map[featureKey]bool
}

type featureKey struct {
	tenant  authz.Scope
	feature string
}

// NewFeatures returns a resolver with nothing switched on.
func NewFeatures() *Features { return &Features{on: map[featureKey]bool{}} }

// Set switches a feature on or off for a tenant scope.
func (f *Features) Set(tenant authz.Scope, feature string, enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.on[featureKey{tenant, feature}] = enabled
}

// Enabled implements authz.FeatureResolver.
func (f *Features) Enabled(_ context.Context, tenant authz.Scope, feature string) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.on[featureKey{tenant, feature}], nil
}
