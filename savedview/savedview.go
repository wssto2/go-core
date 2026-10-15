// Package savedview lets a person keep the filters and search of a list under a name and apply them again with one
// click: the backend of a list's "saved views" menu.
//
//	savedview.Install(app)
//
// What Install puts in the application:
//
//   - the table saved_views (its migration, which the application runs with its migrate command);
//   - three routes under /v1/saved-views (Routes), each for a signed-in person and needing no permission: the person's
//     views of one list (GET ?list=), save one (POST) and delete one (DELETE /:id).
//
// A view is private to the person (SAVEDVIEW-001): the person's id comes from the session and never from the request,
// every read and write is keyed on it, and another person's view cannot be listed, replaced or deleted (a delete of
// it is a 404, the same as a missing id). Saving under a name the person already uses on that list replaces that
// view's state. A person keeps at most 20 views of one list: a new name beyond that is a 400, replacing one is still
// allowed. The state is an opaque JSON object of at most 4000 characters that the list defines and applies; the
// module never reads it. The limits are fixed.
package savedview

import (
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/savedview/core"
	"github.com/wssto2/go-core/savedview/gormstore"
	"github.com/wssto2/go-core/savedview/migrations"
)

// Install puts the saved views into app: the table and the routes.
func Install(app *gocore.App) {
	app.Schema(gocore.Schema{Files: migrations.Files, Models: gormstore.Migrate})

	service := core.New(gormstore.New(app.Database()), app.Clock())

	app.Routes(Declare().To(service)...)
}
