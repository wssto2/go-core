package gocore

import "github.com/wssto2/go-core/authz"

// The two methods below are for features that ship as a module (access,
// identity): they report problems at start-up and set what the whole
// application shares.

// Authorize sets the authorizer that checks the permission of every route that
// Requires one, for a feature that builds the engine itself (access.Install).
// An application has one: setting a second is a start-up problem, and so is
// combining it with WithAuthorizer.
func (a *App) Authorize(authorizer authz.Authorizer) {
	if a.authorizer != nil {
		a.fail("the authorizer was set twice", "set it once: with gocore.WithAuthorizer, or through the feature that builds the engine, such as access.Install")

		return
	}

	a.authorizer = authorizer
}
