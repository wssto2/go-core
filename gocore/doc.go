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
//	    app := gocore.New(gocore.WithPrefix("/api"), gocore.WithDatabase("local"))
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
// New reads the configuration from the environment (see App.Env for your own settings; WithConfig gives one for a
// test). What belongs to the whole site rather than to a feature is an option of New
// or a call on the App: WithDatabase for a named connection, WithSPA, WithRateLimit, WithIdempotency and
// WithTrustedOrigins for the HTTP stack, Root for routes outside the prefix, Disks for named storage
// disks (storage.Use takes them), Use for engine
// middleware that runs before every route, WithCSP for Content-Security-Policy
// sources, WithServiceWorker for /sw.js, and Telemetry for the metrics handle.
//
// An application that is only inspected, never run, is created with Offline: every Install runs, but no database
// is opened (App.Database hands out handles that fail on use), nothing is served, started or migrated, and Run
// returns an error. That is what a generator needs: App.Contracts and App.Catalogues are the installed ones.
//
//	app := gocore.New(gocore.Offline())
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
// (they run like migrate, each declaring its flags on the *Cmd it is given). LoadEnv, called first in main, reads a .env into the
// environment before New reads it.
package gocore
