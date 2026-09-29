package access

import (
	"context"
	"errors"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authzgorm"
	"gorm.io/gorm"
)

// leadColumns says how a lead table expresses the places, the owner and the
// vehicle kind, once, next to the repository.
var leadColumns = authzgorm.Columns{
	Levels:        map[string]string{LevelDealer: "dealer_id", LevelLocation: "location_id"},
	Owner:         "owner_id",
	OwnerLocation: "location_id",
	Attrs:         map[string]string{AttrVehicleKind: "kind"},
}

// Service lists and edits leads. It depends on authz.Authorizer, so its unit
// tests can use authztest.Fake instead of a database of bindings.
type Service struct {
	db   *gorm.DB
	auth authz.Authorizer
}

// NewService returns the service.
func NewService(db *gorm.DB, auth authz.Authorizer) *Service {
	return &Service{db: db, auth: auth}
}

// List returns the leads the caller may see: the access set of sales.lead:view
// (scope, whose, vehicle kind) becomes a WHERE clause. A caller with no access
// gets an empty list, not an error.
func (s *Service) List(ctx context.Context) ([]Lead, error) {
	access, err := s.auth.Access(ctx, LeadView)
	if err != nil {
		return nil, err
	}
	leads := []Lead{}
	err = s.db.WithContext(ctx).Scopes(authzgorm.Filter(access, leadColumns)).Order("id").Find(&leads).Error
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return leads, nil
}

// Get returns one lead if the caller may see it. A lead the caller may not see
// is reported as forbidden.
func (s *Service) Get(ctx context.Context, id int) (Lead, error) {
	lead, err := s.find(ctx, id)
	if err != nil {
		return Lead{}, err
	}
	if err := s.auth.RequireOn(ctx, LeadView, resource(lead)); err != nil {
		return Lead{}, err
	}
	return lead, nil
}

// Rename changes a lead's title if the caller may update it.
func (s *Service) Rename(ctx context.Context, id int, title string) (Lead, error) {
	lead, err := s.find(ctx, id)
	if err != nil {
		return Lead{}, err
	}
	if err := s.auth.RequireOn(ctx, LeadUpdate, resource(lead)); err != nil {
		return Lead{}, err
	}
	lead.Title = title
	if err := s.db.WithContext(ctx).Model(&Lead{}).Where("id = ?", id).Update("title", title).Error; err != nil {
		return Lead{}, apperr.Internal(err)
	}
	return lead, nil
}

func (s *Service) find(ctx context.Context, id int) (Lead, error) {
	var lead Lead
	err := s.db.WithContext(ctx).Take(&lead, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Lead{}, apperr.NotFound("lead not found")
	}
	if err != nil {
		return Lead{}, apperr.Internal(err)
	}
	return lead, nil
}

// resource describes a lead to the engine: where it is, whose it is and its
// vehicle kind. Passing the dealer saves the engine a lookup.
func resource(l Lead) authz.Resource {
	r := authz.Resource{
		Scope:         authz.Scope{Level: LevelLocation, ID: l.LocationID},
		Ancestors:     []authz.Scope{{Level: LevelDealer, ID: l.DealerID}},
		OwnerLocation: l.LocationID,
		Attrs:         map[string]string{AttrVehicleKind: l.Kind},
	}
	if l.OwnerID != nil {
		r.Owner = *l.OwnerID
	}
	return r
}
