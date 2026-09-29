package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/wssto2/go-core/authz"
	"gorm.io/gorm"
)

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
