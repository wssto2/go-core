package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/authz"
	"gorm.io/gorm"
)

// organization is the root place: every dealer is below it.
var organization = authz.Scope{Level: LevelOrganization}

// Places implements authz.ScopeResolver over the dealers and locations tables:
// a location's parent is its dealer, a dealer's parent is the organization.
type Places struct{ db *gorm.DB }

// NewPlaces returns the resolver.
func NewPlaces(db *gorm.DB) *Places { return &Places{db: db} }

// Parent implements authz.ScopeResolver. A scope that does not exist yields
// authz.ErrScopeNotFound, so a binding to a deleted dealer grants nothing.
func (p *Places) Parent(ctx context.Context, s authz.Scope) (authz.Scope, error) {
	switch s.Level {
	case LevelLocation:
		var loc Location
		err := p.db.WithContext(ctx).Take(&loc, s.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return authz.Scope{}, fmt.Errorf("%w: %s", authz.ErrScopeNotFound, s)
		}
		return authz.Scope{Level: LevelDealer, ID: loc.DealerID}, err
	case LevelDealer:
		err := p.db.WithContext(ctx).Take(&Dealer{}, s.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return authz.Scope{}, fmt.Errorf("%w: %s", authz.ErrScopeNotFound, s)
		}
		return authz.Scope{Level: LevelOrganization}, err
	}
	return authz.Scope{}, fmt.Errorf("%w: no parent for %s", authz.ErrInvalidScope, s)
}

// Names implements access.ScopeCatalog: the display name of each given dealer or location that exists.
func (p *Places) Names(ctx context.Context, scopes []authz.Scope) (map[authz.Scope]string, error) {
	out := map[authz.Scope]string{organization: "Organization"}

	var dealers []Dealer
	if err := p.db.WithContext(ctx).Find(&dealers).Error; err != nil {
		return nil, err
	}

	var locations []Location
	if err := p.db.WithContext(ctx).Find(&locations).Error; err != nil {
		return nil, err
	}

	known := map[authz.Scope]string{}
	for _, d := range dealers {
		known[authz.Scope{Level: LevelDealer, ID: d.ID}] = d.Name
	}

	for _, l := range locations {
		known[authz.Scope{Level: LevelLocation, ID: l.ID}] = l.Name
	}

	for _, s := range scopes {
		if name, ok := known[s]; ok {
			out[s] = name
		}
	}

	return out, nil
}

// Options implements access.ScopeCatalog: every dealer, each followed by its locations.
func (p *Places) Options(ctx context.Context) ([]access.ScopeOption, error) {
	var dealers []Dealer
	if err := p.db.WithContext(ctx).Order("id").Find(&dealers).Error; err != nil {
		return nil, err
	}

	var locations []Location
	if err := p.db.WithContext(ctx).Order("id").Find(&locations).Error; err != nil {
		return nil, err
	}

	var out []access.ScopeOption

	for _, d := range dealers {
		dealer := authz.Scope{Level: LevelDealer, ID: d.ID}
		out = append(out, access.ScopeOption{Scope: dealer, Name: d.Name, Parent: organization})

		for _, l := range locations {
			if l.DealerID == d.ID {
				out = append(out, access.ScopeOption{Scope: authz.Scope{Level: LevelLocation, ID: l.ID}, Name: l.Name, Parent: dealer})
			}
		}
	}

	return out, nil
}
