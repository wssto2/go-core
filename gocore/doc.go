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
//	    users := identity.Install(app)
//	    tickets.Install(app, users)
//
//	    app.Run()
//	}
//
// Install only collects (routes, background workers, permissions). Nothing
// runs until Run, which first checks the whole application and reports every
// problem with its fix, then boots, serves and, on a signal, drains and stops.
//
// The application is built on the bootstrap package: old-style
// bootstrap.Modules run in the same App (see App.Modules), so an application
// can move to Install one feature at a time.
package gocore
