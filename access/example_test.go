package access_test

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
)

// exampleApp is an application on an in-memory database with the tables the
// module owns; a real one runs the module's migrations (./app migrate).
func exampleApp() (*gocore.App, func()) {
	reg, cleanup := database.NewTestRegistry("local")
	if err := gormstore.Migrate(reg.MustGet("local")); err != nil {
		panic(err)
	}

	return gocore.New(bootstrap.DefaultConfig(), gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithAuthentication(signedIn)),
		func() { _ = cleanup() }
}

func catalogue() *authz.Catalogue {
	c := authz.NewCatalogue()
	c.MustDefine("tickets.ticket:view")

	return c
}

// Install is the whole setup in the common case: the application's catalogue
// and the people who can hold roles. The module defines its own permissions in
// the catalogue, builds the engine for an application without tenancy, mounts
// the routes and makes the engine the application's authorizer.
func ExampleInstall() {
	app, cleanup := exampleApp()
	defer cleanup()

	cat := catalogue()
	acc := access.Install(app, cat, team{})

	_, defined := cat.Lookup("iam.role:manage")
	fmt.Println(acc.Engine != nil, defined, app.Check())
	// Output: true true <nil>
}

// Seed makes the first administrator: nobody holds the permission to give roles
// yet, so it binds outside the delegation rules, once, from a seed or a command.
func ExampleAccess_Seed() {
	app, cleanup := exampleApp()
	defer cleanup()

	acc := access.Install(app, catalogue(), team{}, access.WithRoles(
		authz.ComputedRole("webmaster", "Webmaster", authz.All()),
	))

	admin := authz.Subject{Kind: authz.KindUser, ID: 1}
	fmt.Println(acc.Seed(context.Background(), admin, "webmaster"))

	err := acc.Engine.Require(authz.WithPrincipal(context.Background(), authz.User(1, 0)), "iam.role:manage")
	fmt.Println(err)
	// Output:
	// <nil>
	// <nil>
}

// WithRoles declares the roles that live in code: identified by key, read-only
// in the editors, bound to people at a place.
func ExampleWithRoles() {
	app, cleanup := exampleApp()
	defer cleanup()

	acc := access.Install(app, catalogue(), team{}, access.WithRoles(
		authz.Role{Key: "viewer", Name: "Viewer", Grants: authz.Grants(authz.QualifierAll, "tickets.ticket:view")},
	))

	role, _ := acc.Engine.PredefinedRole("viewer")
	fmt.Println(role.Name)
	// Output: Viewer
}

// WithPermissions chooses the ids administration is guarded by when the
// application already has names for them.
func ExampleWithPermissions() {
	app, cleanup := exampleApp()
	defer cleanup()

	cat := catalogue()

	ids := access.DefaultPermissions
	ids.ManageBindings, ids.ViewAccess = "team.member:manage", "team.member:view"

	access.Install(app, cat, team{}, access.WithPermissions(ids))

	_, ok := cat.Lookup("team.member:manage")
	_, other := cat.Lookup("iam.user:manage")
	fmt.Println(ok, other)
	// Output: true false
}

// WithScopes gives the application places below the root, with the catalogue
// that resolves and names them.
func ExampleWithScopes() {
	app, cleanup := exampleApp()
	defer cleanup()

	levels, _ := authz.NewHierarchy("organization", "dealer")
	places := &dealers{authztest.NewPlaces().AddDealer(10)}

	acc := access.Install(app, catalogue(), team{}, access.WithScopes(levels, places))
	fmt.Println(acc.Engine.Hierarchy().Levels())
	// Output: [organization dealer]
}

// WithPrefix mounts the routes under a path prefix.
func ExampleWithPrefix() {
	app, cleanup := exampleApp()
	defer cleanup()

	access.Install(app, catalogue(), team{}, access.WithPrefix("/api/v1"))
	fmt.Println(app.Check())
	// Output: <nil>
}

// WithFeatures answers whether a place has a feature on, for permissions
// declared with authz.Feature.
func ExampleWithFeatures() {
	app, cleanup := exampleApp()
	defer cleanup()

	cat := catalogue()
	cat.MustDefine("auction.lot:view", authz.Feature("auction"))

	access.Install(app, cat, team{}, access.WithFeatures(authz.FeatureFunc(
		func(context.Context, authz.Scope, string) (bool, error) { return true, nil })))
	fmt.Println(app.Check())
	// Output: <nil>
}

// WithAudit writes an audit row for every role and binding change, in the
// transaction of the change.
func ExampleWithAudit() {
	reg, cleanup := database.NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	db := reg.MustGet("local")
	_ = gormstore.Migrate(db)
	_ = audit.Migrate(db)

	app := gocore.New(bootstrap.DefaultConfig(), gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithAuthentication(signedIn))
	acc := access.Install(app, catalogue(), team{},
		access.WithRoles(authz.Role{Key: "viewer", Name: "Viewer", Grants: authz.Grants(authz.QualifierAll, "tickets.ticket:view")}),
		access.WithAudit(audit.NewRepository(database.NewTransactor(db))))

	_ = acc.Seed(context.Background(), authz.Subject{Kind: authz.KindUser, ID: 1}, "viewer")

	var n int64

	db.Model(&audit.AuditLog{}).Count(&n)
	fmt.Println(n, "audit row")
	// Output: 1 audit row
}

// On puts the module's tables, and its migrations, on another connection than
// the application's primary one.
func ExampleOn() {
	reg, cleanup := database.NewTestRegistry("local", "shared")
	defer func() { _ = cleanup() }()

	_ = gormstore.Migrate(reg.MustGet("shared"))

	app := gocore.New(bootstrap.DefaultConfig(), gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithAuthentication(signedIn))
	access.Install(app, catalogue(), team{}, access.On("shared"))
	fmt.Println(app.Check())
	// Output: <nil>
}

// NoScopes is the default: the organization is the only place a role is given at.
func ExampleNoScopes() {
	options, _ := access.NoScopes().Options(context.Background())
	fmt.Println(len(options), "places below the organization")
	// Output: 0 places below the organization
}

// Routes is the module's HTTP contract, declared as a value: the TypeScript
// generator reads it without installing anything.
func ExampleRoutes() {
	for _, spec := range access.Routes.Specs()[:2] {
		fmt.Println(spec.Method, spec.Path)
	}
	// Output:
	// GET /iam/roles
	// GET /iam/roles/:ref
}

// DefaultPermissions are the ids used when the application does not choose.
func ExampleDefaultPermissions() {
	fmt.Println(access.DefaultPermissions.ManageBindings)
	// Output: iam.user:manage
}

// dealers is a ScopeCatalog over the test places.
type dealers struct{ *authztest.Places }

func (dealers) Names(context.Context, []authz.Scope) (map[authz.Scope]string, error) { return nil, nil }

func (dealers) Options(context.Context) ([]access.ScopeOption, error) { return nil, nil }
