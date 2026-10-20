// Package core is the framework-free part of the saved-view module: what a saved view is, what a store keeps, and the
// service that applies the rule (SAVEDVIEW-001). It takes the standard library and apperr only.
package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wssto2/go-core/apperr"
)

// MaxPerList caps how many views one person keeps for one list, so the menu stays a short pick list and not an archive.
const MaxPerList = 20

// ErrNotFound is a view that does not exist for the person: another person's view is the same as a missing one.
var ErrNotFound = errors.New("saved view not found")

// View is one person's named set of a list's filters and search. It belongs to that person alone: UserID is the whole
// access rule, and every store read and write is keyed on it.
type View struct {
	ID     int
	UserID int
	// ListKey names the list, such as "vehicles.catalogue".
	ListKey string
	Name    string
	// State is the list's state as a JSON object, opaque to the module: the screen that saved it applies it back.
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store keeps views. The gormstore package is the SQL implementation and storetest the suite every store passes. Every
// method is scoped to one person: a row of somebody else is indistinguishable from a missing one. A method joins the
// transaction in ctx when it carries one (database.TxFor).
type Store interface {
	// List returns the person's views of one list, by name.
	List(ctx context.Context, userID int, listKey string) ([]View, error)
	// FindByName returns the person's view with this name on this list, else ErrNotFound.
	FindByName(ctx context.Context, userID int, listKey, name string) (View, error)
	// Count returns how many views the person keeps for one list.
	Count(ctx context.Context, userID int, listKey string) (int, error)
	// Save inserts a new view (ID 0), setting its ID, or changes the state and update time of an existing one, which
	// must belong to UserID: else ErrNotFound.
	Save(ctx context.Context, view *View) error
	// Delete removes the person's view; ErrNotFound when it is not theirs or does not exist.
	Delete(ctx context.Context, userID, id int) error
}

// Clock tells the time.
type Clock interface{ Now() time.Time }

// Service lists, saves and deletes the acting person's own views. There is no permission: a person may always keep
// views of their own and never see anybody else's, so the person's id is the only authorization and it is never taken
// from the request.
type Service struct {
	store Store
	clock Clock
}

// New returns the service over the store.
func New(store Store, clock Clock) *Service { return &Service{store: store, clock: clock} }

// List returns the person's views of one list, by name.
func (s *Service) List(ctx context.Context, userID int, listKey string) ([]View, error) {
	views, err := s.store.List(ctx, userID, listKey)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	return views, nil
}

// Save (SAVEDVIEW-001) keeps the state under the name, replacing the person's view of the same name on that list. A
// new name is refused (400) once the person keeps MaxPerList views of the list; replacing one is still allowed then.
func (s *Service) Save(ctx context.Context, userID int, listKey, name, state string) (View, error) {
	now := s.clock.Now().UTC().Truncate(time.Second) // the DATETIME column's precision, so the answer is what a read shows

	view, err := s.store.FindByName(ctx, userID, listKey, name)

	switch {
	case err == nil:
		view.State, view.UpdatedAt = state, now
	case errors.Is(err, ErrNotFound):
		count, countErr := s.store.Count(ctx, userID, listKey)
		if countErr != nil {
			return View{}, apperr.Internal(fmt.Errorf("count saved views: %w", countErr))
		}

		if count >= MaxPerList {
			return View{}, apperr.BadRequest(fmt.Sprintf("at most %d saved views per list", MaxPerList))
		}

		view = View{ID: 0, UserID: userID, ListKey: listKey, Name: name, State: state, CreatedAt: now, UpdatedAt: now}
	default:
		return View{}, apperr.Internal(fmt.Errorf("find saved view: %w", err))
	}

	err = s.store.Save(ctx, &view)
	if err != nil {
		return View{}, apperr.Internal(fmt.Errorf("save saved view: %w", err))
	}

	return view, nil
}

// Delete removes one of the person's views. Another person's id is a 404.
func (s *Service) Delete(ctx context.Context, userID, viewID int) error {
	err := s.store.Delete(ctx, userID, viewID)

	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return apperr.NotFound("saved view not found")
	default:
		return apperr.Internal(fmt.Errorf("delete saved view %d: %w", viewID, err))
	}
}
