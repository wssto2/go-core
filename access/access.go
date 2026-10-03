// Package access is roles and access administration for an application: the
// roles people hold, where they hold them, who may give them, and the screens'
// API for all of it. It builds the authz engine of the application and the
// services and routes over it.
//
//	users := identity.Install(app)
//	access.Install(app, permissions.All, users) // roles and bindings
//
// Install defines the module's own permissions in the catalogue when the
// application has not, builds the engine (one hierarchy level, organization,
// unless WithScopes says otherwise), mounts the routes, collects the
// migrations of the three authz tables and makes the engine the application's
// authorizer.
//
// Delegation is the engine's: a person may give a role only where they hold
// the binding permission, never one that holds more than they do, never to
// themselves, and nobody removes their own last binding permission. The
// framework-free services are in access/admin, the routes in access/accesshttp.
package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/wssto2/go-core/access/accesshttp"
	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/authz/migrations"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
)

// The types an application names when it configures the module.
type (
	// ScopeCatalog names the places below the root a role can be given at.
	ScopeCatalog = admin.ScopeCatalog
	// ScopeOption is one such place.
	ScopeOption = admin.ScopeOption
	// SubjectDirectory names the people and service accounts that hold roles.
	SubjectDirectory = admin.SubjectDirectory
)

// The permissions administration is guarded by: iam.role:{view,manage,delete}
// and iam.user:{view,manage}. The ids are fixed.
const (
	ViewRoles      = admin.ViewRoles
	ManageRoles    = admin.ManageRoles
	DeleteRoles    = admin.DeleteRoles
	ViewAccess     = admin.ViewAccess
	ManageBindings = admin.ManageBindings
)

// Routes is the declared HTTP contract (paths under /v1, before the
// application's gocore.WithPrefix): what the TypeScript generator reads, without
// installing anything.
var Routes = accesshttp.Declare().Contract()

// NoScopes is the ScopeCatalog of an application without tenancy.
func NoScopes() ScopeCatalog { return admin.NoScopes() }

// Access is what Install built. After Install reported a problem to the
// application (Run lists it) the value is empty.
type Access struct {
	// Engine is the application's authz engine: it answers Require, RequireOn
	// and Access, and is already the application's authorizer.
	Engine *authz.Engine
	// Roles and Bindings are the administration services the routes call.
	Roles    *admin.Roles
	Bindings *admin.Bindings

	store authz.Store
}

// Install builds roles and access administration into app. catalogue is the
// application's permission catalogue: it must hold every permission the
// application's roles grant, and gets the module's own permissions added when it
// lacks them. users names the people who can hold roles.
//
// Define the application's permissions before Install: building the engine
// freezes the catalogue.
func Install(app *gocore.App, catalogue *authz.Catalogue, users SubjectDirectory, opts ...Option) *Access {
	s := settings{scopes: NoScopes()}
	for _, opt := range opts {
		opt(&s)
	}

	if users == nil {
		app.Fail("access needs a SubjectDirectory", "pass the users as the third argument: access.Install(app, catalogue, users)")

		return &Access{}
	}

	if err := s.define(catalogue); err != nil {
		app.Fail("access cannot define its permissions: "+err.Error(), "define the five permissions yourself before Install, or leave them to it")

		return &Access{}
	}

	app.Permissions(catalogue)

	var conn []database.Connection
	if s.connection != "" {
		conn = append(conn, s.connection)
	}

	db := app.Database(conn...)
	store := gormstore.New(db, s.storeOptions()...)

	engine, err := authz.NewEngine(authz.Config{
		Catalogue: catalogue, Hierarchy: s.hierarchy(), Roles: s.roles, Store: store,
		Resolver: s.scopes, Features: s.features, Logger: app.Logger(),
	})
	if err != nil {
		app.Fail("access cannot build the engine: "+err.Error(),
			"fix the catalogue or the roles named in the message; permissions with a Feature need access.WithFeatures")

		return &Access{}
	}

	roles, bindings, err := admin.New(admin.Config{
		Engine: engine, Store: store, Scopes: s.scopes, Subjects: users,
		Transactor: database.NewTransactor(db),
	})
	if err != nil {
		app.Fail(err.Error(), "see the message")

		return &Access{}
	}

	app.Authorize(engine)
	app.Migrations(migrations.Files, conn...)
	app.Routes(accesshttp.Declare().To(roles, bindings, engine)...)

	return &Access{Engine: engine, Roles: roles, Bindings: bindings, store: store}
}

// ACCESS-ADMIN-001: the first administrator is made outside delegation, by code, never over HTTP.

// Seed gives the subject a predefined role at the root, without any delegation
// check: how the first administrator is made, from a seed or a command, when
// nobody yet holds the permission to give roles. It does nothing when the
// subject already holds the role there.
func (a *Access) Seed(ctx context.Context, subject authz.Subject, role string) error {
	if a.store == nil {
		return errors.New("access: Seed on an Access that Install did not build: see the problems Run reports")
	}

	if _, ok := a.Engine.PredefinedRole(role); !ok {
		return fmt.Errorf("access: no predefined role %q: give the role to access.WithRoles", role)
	}

	_, err := a.store.Bind(ctx, authz.Subject{}, authz.Binding{
		Subject: subject, Role: authz.RoleRef{Key: role}, Scope: a.Engine.Hierarchy().Root(),
	})
	if err != nil && !errors.Is(err, authz.ErrDuplicateBinding) {
		return err
	}

	a.Engine.Evict(subject)

	return nil
}

// Option configures Install.
type Option func(*settings)

type settings struct {
	roles      []authz.Role
	scopes     ScopeCatalog
	levels     *authz.Hierarchy
	features   authz.FeatureResolver
	audit      audit.Repository
	connection database.Connection
}

// WithRoles declares the application's predefined roles: defined in code,
// identified by key, read-only in the editors. Computed roles are allowed.
func WithRoles(roles ...authz.Role) Option {
	return func(s *settings) { s.roles = append(s.roles, roles...) }
}

// WithScopes gives the application places below the root: a hierarchy such as
// authz.NewHierarchy("organization", "dealer", "location") and the catalogue that
// resolves and names them. Without it the root, organization, is the only place.
func WithScopes(h *authz.Hierarchy, scopes ScopeCatalog) Option {
	return func(s *settings) { s.levels, s.scopes = h, scopes }
}

// WithFeatures answers whether a place has a feature switched on, for
// permissions declared with authz.Feature.
func WithFeatures(f authz.FeatureResolver) Option { return func(s *settings) { s.features = f } }

// WithAudit writes an audit row for every role and binding change, in the
// transaction of the change. Build the repository on the module's database:
// audit.NewRepository(database.NewTransactor(db)); the audit_logs table is the
// application's (audit.Migrate).
func WithAudit(repo audit.Repository) Option { return func(s *settings) { s.audit = repo } }

// On puts the module's tables, and so its migrations, on another connection
// than the application's primary one.
func On(conn database.Connection) Option { return func(s *settings) { s.connection = conn } }

func (s settings) hierarchy() *authz.Hierarchy {
	if s.levels != nil {
		return s.levels
	}

	return rootOnly
}

// rootOnly is the hierarchy of an application without tenancy.
var rootOnly = mustHierarchy("organization")

func mustHierarchy(levels ...string) *authz.Hierarchy {
	h, err := authz.NewHierarchy(levels...)
	if err != nil {
		panic(err) // constant, valid input
	}

	return h
}

func (s settings) storeOptions() []gormstore.Option {
	if s.audit == nil {
		return nil
	}

	return []gormstore.Option{gormstore.WithAudit(s.audit)}
}

// define adds the module's permissions the catalogue lacks. Managing needs
// viewing, so a role that grants one grants the other.
func (s settings) define(c *authz.Catalogue) error {
	for _, def := range []struct {
		id   string
		opts []authz.DefineOption
	}{
		{admin.ViewRoles, nil},
		{admin.ManageRoles, []authz.DefineOption{authz.Sensitive(), authz.Requires(admin.ViewRoles)}},
		{admin.DeleteRoles, []authz.DefineOption{authz.Sensitive(), authz.Requires(admin.ViewRoles)}},
		{admin.ViewAccess, nil},
		{admin.ManageBindings, []authz.DefineOption{authz.Sensitive(), authz.Requires(admin.ViewAccess)}},
	} {
		if _, ok := c.Lookup(def.id); ok {
			continue
		}

		if err := c.Define(def.id, def.opts...); err != nil {
			return err
		}
	}

	return nil
}
