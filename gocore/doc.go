// Package gocore assembles an application out of features.
//
// A feature is a package with one Install function. It takes the App, plus
// the other features it needs as ordinary arguments, builds its own services
// and hands what it offers to the App:
//
//	func Install(app *gocore.App, users identity.Users) {
//	    tickets := NewService(NewRepository(app.Database()), users, app.Clock())
//
//	    app.Routes(Show.To(tickets.Show))
//	}
//
// main reads top to bottom, and a feature used before the line that creates
// it does not compile:
//
//	func main() {
//	    app := gocore.New(config.FromEnv())
//
//	    users := identity.Install(app, identity.WithMail(sender), identity.WithCodeSecret(secret))
//	    tickets.Install(app, users)
//
//	    app.Run()
//	}
//
// Install only collects (routes, background workers, permissions). Nothing
// runs until Run, which first checks the whole application and reports every
// problem with its fix, then boots, serves and, on a signal, drains and stops.
//
// What belongs to the whole site rather than to a feature is an option of New
// or a call on the App: Root for routes outside the prefix, Disks for named storage
// disks (storage.Use takes them), Use for engine
// middleware that runs before every route, WithCSP for Content-Security-Policy
// sources, WithServiceWorker for /sw.js, and Telemetry for the metrics handle.
//
// An application that is only inspected, never run, is created with Offline: every Install runs, but no database
// is opened (App.Database hands out handles that fail on use), nothing is served, started or migrated, and Run
// returns an error. That is what a generator needs: App.Contracts and App.Catalogues are the installed ones.
//
//	app := gocore.New(cfg, gocore.Offline())
//	identity.Install(app)
//	tickets.Install(app, users)
//	routes, permissions := app.Contracts(), app.Catalogues()
//
// WithTypeScript adds the built-in command ts:generate, which writes the application's route tables, permission union,
// entities and schemas into frontend/generated from the installed application. New makes the App offline for it by
// itself, from the command line (os.Args[1:], or WithArgs in tests), so main is the same as for serving. The groups of
// go-core's own modules are left out (Shipping).
//
// The command line is Run's: serve, migrate, help, ts:generate and the application's own commands, added with Command
// (they run like migrate, each parsing its own flags). LoadEnv, called first in main, reads a .env into the
// environment before the configuration is.
//
// The application is built on the bootstrap package: old-style
// bootstrap.Modules run in the same App (see App.Modules), so an application
// can move to Install one feature at a time.
package gocore
