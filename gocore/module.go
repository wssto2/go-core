package gocore

import "github.com/wssto2/go-core/authz"

// The two methods below are for features that ship as a module (access,
// identity): they report problems at start-up and set what the whole
// application shares.

// Fail records a start-up problem. Run (and Check) refuses to start and lists
// every recorded problem with its fix, so a feature's Install reports a
// misconfiguration instead of panicking:
//
//	app.Fail("access needs a SubjectDirectory", "pass the users: access.Install(app, catalogue, users)")
func (a *App) Fail(what, fix string) { a.fail(what, fix) }

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
