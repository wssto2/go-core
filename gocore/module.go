package gocore

import "github.com/wssto2/go-core/authz"

// The two methods below are for features that ship as a module (access,
// identity): they report problems at start-up and set what the whole
// application shares.

// Authorize sets the authorizer that checks the permission of every route that
// Requires one, for a feature that builds the engine itself (access.Install).
// Like Authenticate it replaces what WithAuthorizer set, so a test's stand-in
// gives way to the real engine; two features setting it is a start-up problem.
func (a *App) Authorize(authorizer authz.Authorizer) {
	if a.authorizedBy {
		a.fail("two features set the authorizer",
			"install only one feature that builds the engine, such as access.Install, or pass gocore.WithAuthorizer to gocore.New for your own")
	}

	a.authorizedBy = true
	a.authorizer = authorizer
}

// Authorizer returns the authorizer the application has now: the engine
// access.Install built, or what WithAuthorizer set; nil when there is none yet.
// A feature installed before the engine exists (identity) reads it at request
// time, when every Install has run:
//
//	if engine, ok := app.Authorizer().(interface{ MyAccess(context.Context) (authz.MyAccess, error) }); ok { ... }
func (a *App) Authorizer() authz.Authorizer { return a.authorizer }
