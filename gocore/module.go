package gocore

import (
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/route"
)

// The methods below are for features that ship as a module (access,
// identity): they report problems at start-up, set what the whole
// application shares and say which routes are the module's.

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

// Shipping marks everything the caller hands to Routes, Root and RoutesWith until the returned function is called as
// part of go-core's own modules, not the application's. A module's Install starts with it:
//
//	func Install(app *gocore.App, ...) *Users {
//	    defer app.Shipping()()
//
// The route groups (route.Group) of such routes stay in Contracts, but the TypeScript generator (ts:generate)
// leaves them out: the frontend library ships those route tables, so the application's generated folder holds only
// its own.
// Scopes nest. An application's own features do not call it.
func (a *App) Shipping() func() {
	a.shipping++

	return func() { a.shipping-- }
}

// noteShipped records the groups of a route handed over inside a Shipping scope.
func (a *App) noteShipped(handled route.Handled) {
	if a.shipping == 0 {
		return
	}

	if a.shipped == nil {
		a.shipped = map[*route.Contract]bool{}
	}

	for _, c := range route.Contracts(handled) {
		a.shipped[c] = true
	}
}

// OwnContracts lists the groups of Contracts the application's own features installed, leaving out the ones go-core's
// modules did (Shipping): what ts:generate writes route tables for, and what an audit of the application's API
// (contract.Wire) walks.
func (a *App) OwnContracts() []*route.Contract {
	var own []*route.Contract

	for _, c := range a.Contracts() {
		if !a.shipped[c] {
			own = append(own, c)
		}
	}

	return own
}
