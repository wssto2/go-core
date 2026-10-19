package access

import (
	"context"
	"fmt"
	"time"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/gocore"
	idaccount "github.com/wssto2/go-core/identity/account"
	idgorm "github.com/wssto2/go-core/identity/gormstore"
	"gorm.io/gorm"
)

// Install mounts the lead routes over the engine access.Install built (the application's authorizer), and seeds the
// demo data on start-up.
func Install(app *gocore.App, engine authz.Authorizer) {
	db := app.Database()

	// Demo only: a real app creates its tables with migrations and seeds nothing.
	app.OnStart(func(ctx context.Context) error {
		if err := Migrate(db.WithContext(ctx)); err != nil {
			return fmt.Errorf("access: migrate: %w", err)
		}

		if err := seed(ctx, db, app.Clock().Now()); err != nil {
			return fmt.Errorf("access: seed: %w", err)
		}

		return nil
	})

	svc := NewService(db, engine)
	app.Routes(List.To(svc.list), Show.To(svc.show), Rename.To(svc.rename))
}

// PrincipalOf maps the signed-in account to its authz principal, with the account's own location for the
// OwnLocation qualifier. Pass it to identity.WithPrincipal.
func PrincipalOf(db *gorm.DB) func(context.Context, idaccount.Account) (authz.Principal, error) {
	return func(ctx context.Context, a idaccount.Account) (authz.Principal, error) {
		var staff Staff
		_ = db.WithContext(ctx).Take(&staff, a.ID).Error // no row: no own location, which is fine

		return authz.User(a.ID, staff.LocationID), nil
	}
}

// seed creates demo dealers, locations, accounts, bindings and leads once.
func seed(ctx context.Context, db *gorm.DB, now time.Time) error {
	var n int64
	if err := db.WithContext(ctx).Model(&Dealer{}).Count(&n).Error; err != nil || n > 0 {
		return err
	}

	dealers := []Dealer{{ID: 1, Name: "North Motors"}, {ID: 2, Name: "South Motors"}}
	locations := []Location{{ID: 10, DealerID: 1, Name: "North Central"}, {ID: 11, DealerID: 1, Name: "North Harbour"}, {ID: 20, DealerID: 2, Name: "South Central"}}

	for _, rows := range []any{&dealers, &locations} {
		if err := db.WithContext(ctx).Create(rows).Error; err != nil {
			return err
		}
	}

	hash, err := idaccount.Bcrypt{}.Hash("demo")
	if err != nil {
		return err
	}

	accounts := idgorm.New(db).Accounts
	ids := map[string]int{"admin": 1, "anna": 2, "mark": 3}

	for login, id := range ids {
		_, err := accounts.Create(ctx, idaccount.Account{
			ID: id, Login: login, Name: login, Locale: "en", Active: true, PasswordHash: hash, CreatedAt: now,
		})
		if err != nil {
			return err
		}
	}

	staff := []Staff{{UserID: ids["anna"], LocationID: 10}, {UserID: ids["mark"], LocationID: 10}}
	if err := db.WithContext(ctx).Create(&staff).Error; err != nil {
		return err
	}

	store := gormstore.New(db)
	system := authz.Subject{Kind: authz.KindUser, ID: ids["admin"]}

	for _, b := range []struct {
		user  string
		role  string
		scope authz.Scope
	}{
		{"admin", RoleWebmaster, organization},                            // every dealer
		{"anna", RoleSalesperson, authz.Scope{Level: LevelDealer, ID: 1}}, // her own leads at North Motors
		{"mark", RoleManager, authz.Scope{Level: LevelLocation, ID: 10}},  // every lead of North Central only
	} {
		_, err := store.Bind(ctx, system, authz.Binding{
			Subject: authz.Subject{Kind: authz.KindUser, ID: ids[b.user]},
			Role:    authz.RoleRef{Key: b.role}, Scope: b.scope,
		})
		if err != nil {
			return err
		}
	}

	anna, mark := ids["anna"], ids["mark"]
	leads := []Lead{
		{DealerID: 1, LocationID: 10, OwnerID: &anna, Kind: "used", Title: "Anna: used hatchback"},
		{DealerID: 1, LocationID: 10, OwnerID: &mark, Kind: "new", Title: "Mark: new SUV"},
		{DealerID: 1, LocationID: 10, Kind: "used", Title: "Unassigned pool, North Central"},
		{DealerID: 1, LocationID: 11, OwnerID: &anna, Kind: "new", Title: "Anna: new van, North Harbour"},
		{DealerID: 1, LocationID: 11, OwnerID: &mark, Kind: "used", Title: "Mark: used estate, North Harbour"},
		{DealerID: 2, LocationID: 20, Kind: "new", Title: "South Motors: unassigned"},
		{DealerID: 2, LocationID: 20, OwnerID: &mark, Kind: "used", Title: "South Motors: owned by a North user"},
	}

	return db.WithContext(ctx).Create(&leads).Error
}
