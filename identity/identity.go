// Package identity is go-core's sign-in module: accounts, password sign-in,
// sessions, the lock after wrong passwords, signing in as somebody else, and
// the /auth/me payload the client starts from. Install it once:
//
//	app := gocore.New(cfg)
//
//	users := identity.Install(app)                 // routes, tables, authentication
//	access.Install(app, permissions.All, users)    // roles and bindings, and the engine /auth/me reads
//	tickets.Install(app, users)
//
//	app.Run()
//
// identity does not need the authorization engine at Install: it reads the
// application's authorizer when a request comes, so /auth/me carries the
// person's access (authz.MyAccess) as soon as access.Install, or any
// authorizer that has MyAccess, is part of the application, in either order.
// *Users satisfies access.SubjectDirectory, so it is what access.Install takes.
//
// Install gives the application its authentication: every route that is not
// Public is behind it, and the person's authz principal is in the request
// context. It uses the GORM store, bcrypt and the module's own migrations
// unless told otherwise, with options named for what they change:
//
//	users := identity.Install(app,
//		identity.On(Shared),                          // tables on another connection
//		identity.WithNavigation(menu...),             // and the menu, cut to what they may reach
//		identity.AllowImpersonation("iam.user:impersonate"),
//	)
//
// Its routes live under /v1/auth; the application's own prefix goes in front
// (gocore.WithPrefix("/api") serves /api/v1/auth/login).
//
// The framework-free core is identity/account (its types are aliased here);
// identity/gormstore is the default store, identity/http the routes, whose
// contract is Routes, and identity/identitytest what tests use.
package identity

import (
	"context"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/gormstore"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/identity/migrations"
	"github.com/wssto2/go-core/navigation"
)

// The types an application names, aliases of the core's.
type (
	// Account is a person who signs in.
	Account = account.Account
	// Users is what other features ask of identity: Get, ChangeLocale and the sessions of an account.
	Users = account.Users
	// Session is one sign-in of an account on one device.
	Session = account.Session
	// AccountStore keeps accounts; WithAccounts replaces the default.
	AccountStore = account.Store
	// PasswordHasher hashes passwords; WithHasher replaces bcrypt.
	PasswordHasher = account.PasswordHasher
	// Impersonation decides who may sign in as whom; WithImpersonation sets it.
	Impersonation = account.Impersonation
	// Notices hears the facts identity publishes; WithNotices sets it.
	Notices = account.Notices
	// Config tunes the rules: session length, the lock, the attempts per minute.
	Config = account.Config
	// Lock is the lock after wrong passwords.
	Lock = account.Lock
	// UserProjector makes the session payload's user of an account.
	UserProjector = identityhttp.UserProjector
	// NavigationProvider returns the application's menu for an account.
	NavigationProvider = identityhttp.NavigationProvider
	// PrincipalOf is the authz principal of an account.
	PrincipalOf = identityhttp.PrincipalOf
	// Cookies name and place the cookies the tokens travel in.
	Cookies = identityhttp.Cookies
)

// Routes is identity's declared contract: the routes it serves, for
// contract.Generate and for reading without an application.
var Routes = identityhttp.Routes

// Access is what identity asks of the authorization engine for the payload's
// access block. *authz.Engine is one.
type Access interface {
	MyAccess(ctx context.Context) (authz.MyAccess, error)
}

type settings struct {
	conn                    []database.Connection
	accounts                account.Store
	hasher                  account.PasswordHasher
	refresh                 auth.Hasher
	cfg                     account.Config
	impersonation           account.Impersonation
	impersonationPermission string
	notices                 account.Notices
	access                  Access
	project                 UserProjector
	principal               PrincipalOf
	navigation              NavigationProvider
	cookies                 Cookies
}

// Option adjusts Install.
type Option func(*settings)

// On puts identity's tables on a connection other than the primary one, and
// its migrations with them.
func On(conn database.Connection) Option {
	return func(s *settings) { s.conn = []database.Connection{conn} }
}

// WithAccounts keeps the accounts in your own store, for example over a table
// that already exists, instead of the default accounts table.
func WithAccounts(store account.Store) Option { return func(s *settings) { s.accounts = store } }

// WithHasher replaces bcrypt, for passwords stored in another format.
func WithHasher(h account.PasswordHasher) Option { return func(s *settings) { s.hasher = h } }

// WithRefreshHasher sets how refresh tokens are hashed at rest. The default
// (SHA-256) suits a random 256-bit token; set it to keep reading sessions an
// application already stored with another hasher, such as auth.NewHMACHasher.
func WithRefreshHasher(h auth.Hasher) Option { return func(s *settings) { s.refresh = h } }

// WithConfig sets the session length, the lock and the attempts per minute.
func WithConfig(cfg account.Config) Option { return func(s *settings) { s.cfg = cfg } }

// WithAccess overrides where /auth/me reads how the person holds each
// permission. By default it asks the application's authorizer (the engine
// access.Install builds) when it has MyAccess; without one the access block
// holds no permissions.
func WithAccess(a Access) Option { return func(s *settings) { s.access = a } }

// AllowImpersonation lets people who hold the permission sign in as somebody
// else. The check goes through the application's authorizer when the request
// comes, so the permission must be in the catalogue access.Install gets.
// Without this or WithImpersonation, nobody may.
func AllowImpersonation(permission string) Option {
	return func(s *settings) { s.impersonationPermission = permission }
}

// WithImpersonation decides who may sign in as whom with your own rules, such
// as limiting a person to targets who can do no more than they can.
func WithImpersonation(i account.Impersonation) Option {
	return func(s *settings) { s.impersonation = i }
}

// WithNotices hears the facts identity publishes (signing in as somebody,
// sessions ended), to write an audit trail or send a notification.
func WithNotices(n account.Notices) Option { return func(s *settings) { s.notices = n } }

// WithUserProjector makes the payload's user of an account yourself, to carry
// a name, a role or a photo. The result must serialise with an "id".
func WithUserProjector(p UserProjector) Option { return func(s *settings) { s.project = p } }

// WithPrincipal says which authz principal an account acts as, for an
// application whose people have a location of their own.
func WithPrincipal(p PrincipalOf) Option { return func(s *settings) { s.principal = p } }

// WithNavigation sets the menu /auth/me carries, cut down to the permissions the
// person holds. The tree is yours; identity only filters it.
func WithNavigation(menu ...navigation.Node) Option {
	return func(s *settings) {
		s.navigation = func(context.Context, account.Account) ([]navigation.Node, error) { return menu, nil }
	}
}

// WithNavigationProvider is WithNavigation for a menu that depends on the person.
func WithNavigationProvider(p NavigationProvider) Option {
	return func(s *settings) { s.navigation = p }
}

// WithCookies renames and places the token cookies.
func WithCookies(c Cookies) Option { return func(s *settings) { s.cookies = c } }

// Install puts identity into the application: its tables (migrations, which
// the application runs with "./app migrate"), its routes and the authentication
// every other route is behind. It returns what other features need.
func Install(app *gocore.App, opts ...Option) *Users {
	var s settings
	for _, opt := range opts {
		opt(&s)
	}

	var storeOpts []gormstore.Option
	if s.refresh != nil {
		storeOpts = append(storeOpts, gormstore.WithRefreshHasher(s.refresh))
	}

	stores := gormstore.New(app.Database(s.conn...), storeOpts...)

	app.Schema(gocore.Schema{Files: migrations.Files, Models: gormstore.Migrate}, s.conn...)

	if s.accounts == nil {
		s.accounts = stores.Accounts
	}

	if s.impersonationPermission != "" && s.impersonation == nil {
		s.impersonation = permitted{app: app, permission: s.impersonationPermission}
	}

	svc, err := account.New(account.Deps{
		Accounts: s.accounts, SignIns: stores.SignIns, Sessions: stores.Sessions, Clock: app.Clock(),
		Hasher: s.hasher, Impersonation: s.impersonation, Notices: s.notices,
	}, s.cfg)
	if err != nil {
		app.Fail("identity could not be installed: "+err.Error(), "check the options given to identity.Install")

		return &account.Users{}
	}

	cfg := identityhttp.Config{
		Services: svc, Clock: app.Clock(), Cookies: s.cookies,
		Project: s.project, Principal: s.principal, Navigation: s.navigation,
	}

	cfg.Access = s.accessOf(app)

	app.Authenticate(identityhttp.Authentication(svc.SignIn, s.cookies, s.principal))
	app.Routes(identityhttp.NewHandler(cfg).Routes()...)

	return svc.Users
}

// accessOf is where the payload's access block comes from: the override, else
// the application's authorizer at request time, else nothing (the payload then
// holds the subject and no permissions).
func (s settings) accessOf(app *gocore.App) identityhttp.AccessProvider {
	if s.access != nil {
		return s.access.MyAccess
	}

	return func(ctx context.Context) (authz.MyAccess, error) {
		if a, ok := app.Authorizer().(Access); ok {
			return a.MyAccess(ctx)
		}

		p, _ := authz.PrincipalFrom(ctx)

		return authz.MyAccess{Subject: p.Subject, Permissions: map[string]authz.PermissionAccess{}}, nil
	}
}

// permitted is the Impersonation of a permission: whoever holds it may sign in
// as anybody. Narrower rules are WithImpersonation's.
type permitted struct {
	app        *gocore.App
	permission string
}

func (p permitted) Permitted(ctx context.Context, _ account.Account) error {
	authorizer := p.app.Authorizer()
	if authorizer == nil {
		return apperr.Forbidden(string(account.ReasonImpersonationDisabled)).WithReason(account.ReasonImpersonationDisabled)
	}

	return authorizer.Require(ctx, p.permission)
}

func (permitted) Covers(context.Context, account.Account, account.Account) error { return nil }
