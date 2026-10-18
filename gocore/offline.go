package gocore

import (
	"errors"
	"log/slog"
	"os"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
)

var errOffline = errors.New("gocore: the application is offline (gocore.Offline): it opens no database and does not serve")

// Offline makes an App that can be assembled and inspected but never run, for what needs the installed application and
// nothing outside it: a generator that writes TypeScript from App.Contracts and App.Catalogues, a documentation
// command, a test of the wiring. Every Install runs exactly as for serving; what is different is what the App touches:
//
//   - No database is opened, whatever the config lists, and WithRegistry is ignored. App.Database returns, for every
//     connection, a handle whose queries all fail with an error saying the App is offline, so a feature can build its
//     repositories at install time and query only when a request comes, as it does when serving. No problem is
//     recorded for the missing database, and Events and Schema work without one (migrations are collected, never run).
//   - Serving and migrating are refused: Run and RunContext without a command, and migrate, migrate status and
//     migrate mark-applied, return an error. Commands an application adds, and help, work; they get the handles above.
//   - OnStart checks and background workers are collected and never run, and no listener is opened.
//   - The logger writes warnings and errors to standard error, no log directory or file is created (the default
//     logger would create one and write to standard output, where a generator writes its result); WithLogger replaces it.
//   - Telemetry stays: it is an in-process registry. Disks stay as declared: storage.Disk(...).Local(root) creates
//     root when the spec is built, before the App sees it, so declare disks a generator does not need conditionally,
//     or with Memory().
//
// Check works as ever and reports the same wiring problems (an unset Later, a permission no catalogue defines, a
// route that declares no access): it reads no database. Handler works too.
func Offline() Option {
	return func(app *App) { app.offline = true }
}

// Catalogues lists the permission catalogues the installed features handed to Permissions, in the order they were
// added: what the TypeScript generator writes the permission union from.
func (a *App) Catalogues() []*authz.Catalogue {
	return append([]*authz.Catalogue(nil), a.catalogues...)
}

// offlineLogger is the logger of an offline App: no files, nothing on standard output.
func offlineLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// offlineDatabase is the one handle an offline App hands out for every connection.
func (a *App) offlineDatabase() *gorm.DB {
	if a.offlineDB == nil {
		a.offlineDB = database.Unavailable(errOffline)
	}

	return a.offlineDB
}
