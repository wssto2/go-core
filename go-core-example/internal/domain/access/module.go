package access

import (
	"context"
	"fmt"

	domainauth "go-core-example/internal/domain/auth"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/audit"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authzhttp"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Module wires authorization and the lead routes into the application. Register
// it after the auth module (it seeds two demo users next to the auth module's
// admin).
type Module struct{}

// NewModule returns the module.
func NewModule() *Module { return &Module{} }

// Name implements bootstrap.Module.
func (*Module) Name() string { return "access" }

// Register builds the engine and mounts /api/v1/leads and /api/v1/me/access.
func (*Module) Register(c *bootstrap.Container) error {
	db := bootstrap.MustResolve[*database.Registry](c).Primary()
	if err := Migrate(db); err != nil {
		return fmt.Errorf("access: migrate: %w", err)
	}
	if err := gormstore.Migrate(db); err != nil {
		return fmt.Errorf("access: migrate authz: %w", err)
	}
	if err := audit.Migrate(db); err != nil {
		return fmt.Errorf("access: migrate audit: %w", err)
	}

	// Every role and binding change is audited, in the same transaction.
	store := gormstore.New(db, gormstore.WithAudit(bootstrap.MustResolve[audit.Repository](c)))
	engine, err := newEngine(db, store)
	if err != nil {
		return err
	}
	if err := seed(context.Background(), db, store); err != nil {
		return fmt.Errorf("access: seed: %w", err)
	}

	eng, err := bootstrap.Resolve[*gin.Engine](c)
	if err != nil {
		return fmt.Errorf("access: resolve engine: %w", err)
	}
	api := eng.Group("/api/v1")
	api.Use(coreauth.Authenticated(bootstrap.MustResolve[coreauth.Provider](c)))
	api.Use(authzhttp.Principals(principalOf(db)))
	api.Use(authzhttp.PinTenant(engine)) // tenancy.ScopeByTenant keeps working as a second wall
	RegisterRoutes(api, NewService(db, engine), engine)
	return nil
}

// Boot implements bootstrap.Module.
func (*Module) Boot(context.Context) error { return nil }

// Shutdown implements bootstrap.Module.
func (*Module) Shutdown(context.Context) error { return nil }

func newEngine(db *gorm.DB, store authz.Reader) (*authz.Engine, error) {
	cat, err := NewCatalogue()
	if err != nil {
		return nil, err
	}
	h, err := NewHierarchy()
	if err != nil {
		return nil, err
	}
	return authz.NewEngine(authz.Config{
		Catalogue: cat, Hierarchy: h, Roles: Roles(), Store: store, Resolver: NewPlaces(db),
	})
}

// principalOf maps the signed-in user to a principal, with the user's own
// location for the OwnLocation qualifier.
func principalOf(db *gorm.DB) func(context.Context, coreauth.Identifiable) (authz.Principal, bool) {
	return func(ctx context.Context, identity coreauth.Identifiable) (authz.Principal, bool) {
		var staff Staff
		_ = db.WithContext(ctx).Take(&staff, identity.GetID()).Error // no row: no own location, which is fine
		return authz.User(identity.GetID(), staff.LocationID), true
	}
}

// seed creates demo dealers, locations, users, bindings and leads once.
func seed(ctx context.Context, db *gorm.DB, store *gormstore.Store) error {
	var n int64
	if err := db.Model(&Dealer{}).Count(&n).Error; err != nil || n > 0 {
		return err
	}
	dealers := []Dealer{{ID: 1, Name: "North Motors"}, {ID: 2, Name: "South Motors"}}
	locations := []Location{{ID: 10, DealerID: 1, Name: "North Central"}, {ID: 11, DealerID: 1, Name: "North Harbour"}, {ID: 20, DealerID: 2, Name: "South Central"}}
	for _, rows := range []any{&dealers, &locations} {
		if err := db.Create(rows).Error; err != nil {
			return err
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("demo"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	users := map[string]*domainauth.User{"admin": {}, "anna": {}, "mark": {}}
	for name, u := range users {
		*u = domainauth.User{Username: name, PasswordHash: string(hash)}
		if err := db.Where("username = ?", name).FirstOrCreate(u).Error; err != nil {
			return err
		}
	}

	system := authz.Subject{Kind: authz.KindUser, ID: users["admin"].ID}
	staff := []Staff{{UserID: users["anna"].ID, LocationID: 10}, {UserID: users["mark"].ID, LocationID: 10}}
	if err := db.Create(&staff).Error; err != nil {
		return err
	}
	for _, b := range []struct {
		user  string
		role  string
		scope authz.Scope
	}{
		{"admin", RoleWebmaster, authz.Scope{Level: LevelOrganization}},   // every dealer
		{"anna", RoleSalesperson, authz.Scope{Level: LevelDealer, ID: 1}}, // her own leads at North Motors
		{"mark", RoleManager, authz.Scope{Level: LevelLocation, ID: 10}},  // every lead of North Central only
	} {
		_, err := store.Bind(ctx, system, authz.Binding{
			Subject: authz.Subject{Kind: authz.KindUser, ID: users[b.user].ID},
			Role:    authz.RoleRef{Key: b.role}, Scope: b.scope,
		})
		if err != nil {
			return err
		}
	}

	anna, mark := users["anna"].ID, users["mark"].ID
	leads := []Lead{
		{DealerID: 1, LocationID: 10, OwnerID: &anna, Kind: "used", Title: "Anna: used hatchback"},
		{DealerID: 1, LocationID: 10, OwnerID: &mark, Kind: "new", Title: "Mark: new SUV"},
		{DealerID: 1, LocationID: 10, Kind: "used", Title: "Unassigned pool, North Central"},
		{DealerID: 1, LocationID: 11, OwnerID: &anna, Kind: "new", Title: "Anna: new van, North Harbour"},
		{DealerID: 1, LocationID: 11, OwnerID: &mark, Kind: "used", Title: "Mark: used estate, North Harbour"},
		{DealerID: 2, LocationID: 20, Kind: "new", Title: "South Motors: unassigned"},
		{DealerID: 2, LocationID: 20, OwnerID: &mark, Kind: "used", Title: "South Motors: owned by a North user"},
	}
	return db.Create(&leads).Error
}
