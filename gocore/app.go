package gocore

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/env"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/logger"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/observability"
	"github.com/wssto2/go-core/ratelimit"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/storage"
	"github.com/wssto2/go-core/tenancy"
	"github.com/wssto2/go-core/web"
	"github.com/wssto2/go-core/worker"
	"gorm.io/gorm"
)

// Clock tells the time. Features take it from App.Clock instead of calling
// time.Now, so tests can fix it.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Problem is one thing that stops the application from starting: what is
// wrong and what to do about it.
type Problem struct {
	What string
	Fix  string
}

func (p Problem) String() string { return p.What + ". Fix: " + p.Fix }

// StartupError lists every problem found before the application started.
type StartupError struct {
	Problems []Problem
}

func (e *StartupError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "gocore: cannot start, %d problem(s):", len(e.Problems))

	for i, p := range e.Problems {
		fmt.Fprintf(&b, "\n  %d. %s", i+1, p)
	}

	return b.String()
}

// App is the application under assembly. Features read its environment
// (Database, Clock, Config, Logger) and hand it what they offer (Routes,
// Background, Permissions). Create it with New; start it with Run.
//
// An App is built on one goroutine, in main or in a test, and is not safe for
// concurrent Install calls.
type App struct {
	cfg          Config
	cfgSet       bool // WithConfig was given, so the environment is not read into cfg
	env          *env.Reader
	databases    []declaredDatabase
	log          *slog.Logger
	clock        Clock
	tel          *observability.Telemetry
	registry     *database.Registry
	authorizer   authz.Authorizer
	authenticate []gin.HandlerFunc
	middleware   []gin.HandlerFunc
	// onStart are the checks serving runs before it starts.
	onStart []func(ctx context.Context) error

	// The site's HTTP options, applied by build.
	spa            *spaOption
	perUserLimit   ratelimit.Limiter
	perIPLimit     ratelimit.Limiter
	globalLimit    ratelimit.Limiter
	idempotencyTTL time.Duration
	trustedOrigins []string
	csp            []gin.HandlerFunc

	disks      *storage.Registry
	routes     []mounted
	workers    []worker.Worker
	catalogues []*authz.Catalogue
	consumers  []event.Consumer
	// eventsUsed is set once a feature called Events, with or without consumers.
	eventsUsed bool
	// deadLetterRetention is how long the event housekeeper keeps dead letters; zero is for ever.
	deadLetterRetention time.Duration

	// commands are the ones added with Command, besides migrate and help.
	commands []appCommand

	migrations []migrationSource
	// autoMigrate, when set, applies migrations as they are collected.
	autoMigrate context.Context //nolint:containedctx // test option: the context of the test

	// offline is set by Offline, or by New for ts:generate: no database, no serving.
	offline   bool
	offlineDB *gorm.DB

	// args and argsSet are WithArgs: the command line New and Run read instead of os.Args[1:].
	args    []string
	argsSet bool
	// ts is WithTypeScript, which also enables the ts:generate command.
	ts *TS
	// shipping counts the open Shipping scopes; shipped are the route groups installed inside one: go-core's own.
	shipping int
	shipped  map[*route.Contract]bool

	prefix   string
	problems []Problem
	laters   []unsetter
	started  bool
	// authenticatedBy is set once a feature has called Authenticate.
	authenticatedBy bool
	// authorizedBy is set once a feature has called Authorize.
	authorizedBy bool
}

// Option adjusts New.
type Option func(*App)

// WithAuthorizer sets the authorizer that checks the permission of every
// route declared with Requires. Without one, such a route stops start-up.
func WithAuthorizer(a authz.Authorizer) Option {
	return func(app *App) { app.authorizer = a }
}

// WithAuthentication sets how requests are authenticated, once for the whole
// application. Every route is mounted behind it unless declared Public. Pass
// the middleware that verifies the credentials and the one that puts the authz
// principal on the request, in that order:
//
//	gocore.WithAuthentication(auth.Authenticated(provider), authzhttp.Principals(resolve))
//
// The App then puts the signed-in person's tenant in the request context, as
// tenancy.FromAuthenticatedUser does (always on: a person who is not
// tenancy.TenantAware gets nothing, and Public routes are not touched).
//
// An unauthenticated request must be rejected with apperr.Unauthorized, as
// those do. Without this option, any route that is not Public stops start-up.
func WithAuthentication(middleware ...gin.HandlerFunc) Option {
	return func(app *App) { app.authenticate = append(app.authenticate, middleware...) }
}

// WithPrefix mounts every route collected by Routes under one path prefix, the
// place the API lives: WithPrefix("/api") serves a route declared as
// "/v1/iam/roles" at "/api/v1/iam/roles". The prefix does not carry a version:
// routes declare theirs. Modules never take a prefix of their own. Root routes
// are the exception: they are served outside it.
func WithPrefix(prefix string) Option {
	return func(app *App) { app.prefix = "/" + strings.Trim(prefix, "/") }
}

// WithDeadLetterRetention makes the event housekeeper, which Events adds, delete dead letters (and the events only they
// kept) d after the consumer gave up on them, instead of keeping them for ever. It is an option of New, like WithClock,
// so the host configures the one housekeeper the application has; see event.Housekeeper.KeepDeadLetters.
func WithDeadLetterRetention(d time.Duration) Option {
	return func(app *App) { app.deadLetterRetention = d }
}

// WithCSP adds sources to a directive of the Content-Security-Policy, and
// creates the directive when the policy has none. Sources are written as in a
// policy, quotes included:
//
//	gocore.WithCSP("frame-src", "'self'", "https://www.google.com/maps/")
//
// The development policy already allows what Vite and vue-core need (HMR,
// 'unsafe-eval' for Zod); production keeps a nonce-only script-src.
func WithCSP(directive string, sources ...string) Option {
	return func(app *App) { app.csp = append(app.csp, middlewares.CSPSources(directive, sources...)) }
}

// WithServiceWorker serves the file at /sw.js, at the root of the site outside
// the prefix, which is the scope a PWA's service worker needs to control every
// page. It is public and never cached (Cache-Control: no-cache), so a new
// build's worker is picked up on the next load. A missing file answers 404.
func WithServiceWorker(file string) Option {
	return func(app *App) {
		app.Root(route.Raw(http.MethodGet, "/sw.js").Name("serviceworker").Public().To(func(ctx *gin.Context) {
			ctx.Header("Cache-Control", "no-cache")
			ctx.Header("Content-Type", "text/javascript; charset=utf-8")
			ctx.File(file)
		}))
	}
}

// WithClock replaces the system clock.
func WithClock(c Clock) Option {
	return func(app *App) { app.clock = c }
}

// WithLogger replaces the logger built from the log config.
func WithLogger(log *slog.Logger) Option {
	return func(app *App) { app.log = log }
}

// WithAutoMigrate applies migrations the moment they are collected, on ctx,
// instead of leaving it to Migrate. It is for tests, where installing a
// feature should leave its tables ready; gocoretest.New sets it.
func WithAutoMigrate(ctx context.Context) Option {
	return func(app *App) { app.autoMigrate = ctx }
}

// WithRegistry uses reg instead of opening the connections listed in the
// config. The caller closes it when the App is never run.
func WithRegistry(reg *database.Registry) Option {
	return func(app *App) { app.registry = reg }
}

// WithConfig uses cfg instead of reading the environment, for tests and for a host that does not configure itself
// from the environment. Connections declared with WithDatabase are still read from the environment and added to it.
func WithConfig(cfg Config) Option {
	return func(app *App) { app.cfg, app.cfgSet = cfg, true }
}

// New creates an App. It reads its configuration from the environment (see Config; APP_ENV, APP_PORT, LOG_*,
// TRUSTED_PROXIES, and the connections declared with WithDatabase), unless WithConfig gives one. A value that cannot be
// read, opening the logger and opening the database connections can fail; the failure is held and reported by Run
// with the other start-up problems.
func New(opts ...Option) *App {
	app := new(App)
	app.clock, app.env = systemClock{}, env.New(os.LookupEnv)

	for _, opt := range opts {
		opt(app)
	}

	if !app.offline && asksForOffline(app.commandLine()) {
		app.offline = true
	}

	if app.offline {
		app.registry = nil
	}

	app.configure()
	app.openLogger()
	app.tel = observability.New(app.log, app.cfg.App.Name)
	app.openRegistry()

	return app
}

// Env reads settings from the process environment with explicit calls: Required, String, Int, Bool, Duration and
// List. A missing required value or a malformed one is a start-up problem that names the variable, listed by Run and
// Check with the others, so nothing starts half-loaded:
//
//	secret := app.Env().Required("EMBED_SECRET")
//	limit := app.Env().Int("IMPORT_BATCH", 500)
//
// A variable set to the empty string counts as unset. See package env, which gives the same reader outside an App.
func (a *App) Env() *env.Reader { return a.env }

// security is what routes are mounted behind.
func (a *App) security() route.Security {
	var chain []gin.HandlerFunc
	if len(a.authenticate) > 0 {
		// The tenant follows the sign-in, so hooks and handlers of every route read tenant-scoped data as the person does.
		chain = append(append(chain, a.authenticate...), tenancy.FromAuthenticatedUser())
	}

	return route.Security{Authenticate: chain, Authorizer: a.authorizer}
}

func (a *App) fail(what, fix string) {
	a.problems = append(a.problems, Problem{What: what, Fix: fix})
}

// Prefix is the path prefix every route is served under ("" for none, "/api"
// for WithPrefix("/api")), for what must name the API's path, such as a cookie.
func (a *App) Prefix() string {
	if a.prefix == "/" {
		return ""
	}

	return a.prefix
}

// Disks declares the application's named storage disks. A disk is private unless it says Public; features take one
// with storage.Use(app, name), so declare the disks before installing them:
//
//	app.Disks(
//	    storage.Disk("uploads").Local("storage/uploads"),                // served only through the app
//	    storage.Disk("public").Local("storage/public").Public("/media"), // anyone with the URL can open it
//	)
//
// A disk made Public with a path is served by the application at that path, outside the prefix: GET and HEAD only, the
// file and never a directory listing, and nothing outside the disk's root. A Public absolute URL ("https://cdn…") is
// served by whatever is behind it, so nothing is mounted.
//
// A disk without a name or a driver, a name declared twice, or a root that cannot be used stops start-up.
func (a *App) Disks(disks ...storage.DiskSpec) {
	if a.disks == nil {
		a.disks = storage.NewRegistry()
	}

	for _, d := range disks {
		err := a.disks.Add(d)
		if err != nil {
			a.fail(err.Error(), "give each disk in app.Disks a unique name and Local(root), Memory() or Driver(d)")

			continue
		}

		a.serveDisk(d)
	}
}

func (a *App) serveDisk(spec storage.DiskSpec) {
	files, _ := a.disks.Disk(spec.Name())

	base := files.PublicPath()
	if base == "" {
		return
	}

	serve := func(ctx *gin.Context) { web.Serve(ctx, files, strings.TrimPrefix(ctx.Param("key"), "/"), "") }

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		a.Root(route.Raw(method, base+"/*key").Name("disk." + spec.Name() + "." + strings.ToLower(method)).Public().To(serve))
	}
}

// Disk returns the disk declared as name; storage.Use is the way features ask for one.
func (a *App) Disk(name string) (*storage.Files, bool) {
	if a.disks == nil {
		return nil, false
	}

	return a.disks.Disk(name)
}

// Fail reports a problem that stops the application from starting, for a
// feature whose Install found something it cannot continue with. Run and
// Check list it with the other problems; what says what is wrong and fix what
// to do about it.
func (a *App) Fail(what, fix string) { a.fail(what, fix) }

// Authenticate sets how requests are authenticated, from a feature that does it
// itself (identity.Install does). Like WithAuthentication it is for the whole
// application and replaces what the options set, so a test's stand-in gives way
// to the real thing. Two features authenticating is a start-up problem: an
// application is authenticated one way.
func (a *App) Authenticate(middleware ...gin.HandlerFunc) {
	if a.authenticatedBy {
		a.fail("two features set how requests are authenticated",
			"install only one authenticating feature, such as identity.Install, or pass gocore.WithAuthentication to gocore.New for your own")
	}

	a.authenticatedBy = true
	a.authenticate = append([]gin.HandlerFunc(nil), middleware...)
}

// Use adds engine middleware that runs before every route, those of modules
// included, in the order given, whenever it is called: a middleware added after
// Routes still runs before them. It runs after the request id, logging,
// security headers and rate limits the App sets up, and before authentication,
// so it sees every request, signed in or not.
func (a *App) Use(middleware ...gin.HandlerFunc) {
	a.middleware = append(a.middleware, middleware...)
}

// OnStart adds a check that serving runs before it starts, once the migrations
// are known to be applied: what the application needs of its database (a
// config row it cannot run without, data it reads once per process). An error
// stops start-up and is listed with the other problems. Commands (migrate,
// help) and Check do not run it, so they work on a database with no schema.
func (a *App) OnStart(check func(ctx context.Context) error) {
	a.onStart = append(a.onStart, check)
}

// Config returns the configuration the App runs with.
func (a *App) Config() Config { return a.cfg }

// Environment is the application environment: "production", "staging" or "development" (an unset APP_ENV is
// development).
func (a *App) Environment() string {
	if a.cfg.App.Env == "" {
		return EnvDevelopment
	}

	return a.cfg.App.Env
}

// Logger returns the application logger.
func (a *App) Logger() *slog.Logger { return a.log }

// Clock returns the application clock.
func (a *App) Clock() Clock { return a.clock }

// Telemetry returns the metrics handle of the application: the one behind
// /metrics and the HTTP metrics, which workers and features record to as well.
func (a *App) Telemetry() *observability.Telemetry { return a.tel }

// Database returns the primary connection, or with an argument the named one.
// A connection that is not registered is reported by Run, listing the
// registered ones; until then the returned handle fails every query.
func (a *App) Database(conn ...database.Connection) *gorm.DB {
	if len(conn) > 1 {
		a.fail("Database was given more than one connection", "call Database once per connection")
	}

	if a.offline {
		return a.offlineDatabase()
	}

	if a.registry == nil {
		a.fail("a feature asked for the database but none is configured",
			"add a connection to the database config, or pass gocore.WithRegistry")

		return database.Unavailable(errNoDatabase)
	}

	if len(conn) == 0 {
		name := a.registry.PrimaryName()
		if name == "" {
			a.fail("a feature asked for the primary database but no connection is registered",
				"add a connection to the database config")

			return database.Unavailable(errNoDatabase)
		}

		return a.registry.MustGet(name)
	}

	db, err := a.registry.Database(conn[0])
	if err != nil {
		a.fail(err.Error(), "register the connection in the database config, or fix its name")

		return database.Unavailable(err)
	}

	return db
}

// Routes collects the routes a feature serves. They are mounted by Run.
func (a *App) Routes(routes ...route.Handled) {
	for _, r := range routes {
		a.routes = append(a.routes, mounted{Handled: r, own: nil})
		a.noteShipped(r)
	}
}

// Root collects routes served outside the prefix, at the root of the site: a
// service worker at /sw.js, an embed page at /embed. They are mounted behind
// the same security as Routes, unless a route is Public.
func (a *App) Root(routes ...route.Handled) {
	for _, r := range routes {
		a.routes = append(a.routes, mounted{Handled: r, own: nil, root: true})
		a.noteShipped(r)
	}
}

// RoutesWith collects routes that are authenticated by a chain of the feature's
// own instead of the application's: an app whose routes take API keys or its
// own tokens next to identity's session, for instance.
//
//	app.RoutesWith(route.Security{Authenticate: []gin.HandlerFunc{auth.Authenticated(provider)}}, routes...)
//
// Everything else is as for Routes: Public routes stay open, Requires is checked
// by the authorizer (the application's, unless security sets one) and a
// permission no catalogue defines stops start-up. The 401 a failing chain
// answers is the chain's own. The tenant is not put in the context: the chain
// does that, if it wants. The routes of go-core's modules (identity, access,
// notification) always keep the application's chain. A security without an Authenticate chain does not fall back to the
// application's: its non-public routes stop start-up; use Routes for those.
func (a *App) RoutesWith(security route.Security, routes ...route.Handled) {
	for _, r := range routes {
		a.routes = append(a.routes, mounted{Handled: r, own: &security})
		a.noteShipped(r)
	}
}

// Background collects workers that run for the life of the application: they
// start at boot and are drained at shutdown. Their context is marked all-tenants
// (tenancy.WithAllTenants), since a worker acts for nobody; a worker acting for one
// tenant narrows it with tenancy.WithTenantID.
func (a *App) Background(workers ...worker.Worker) {
	a.workers = append(a.workers, workers...)
}

// Permissions adds a catalogue of permissions. Run checks that every
// permission a route Requires is defined in one of them.
func (a *App) Permissions(catalogue *authz.Catalogue) {
	a.catalogues = append(a.catalogues, catalogue)
}

// mounted is a collected route with the security it asked for, if any.
type mounted struct {
	route.Handled

	own *route.Security
	// root routes are served outside the prefix.
	root bool
}

// path is the route's path as served.
func (m mounted) path(prefix string) string {
	if m.root {
		return m.Spec().Path
	}

	return prefix + m.Spec().Path
}

// mount registers the route under the prefix, or at the site root, behind the application's security, or its own.
func (m mounted) mount(engine gin.IRouter, prefix string, app route.Security) error {
	var group gin.IRoutes = engine
	if !m.root {
		group = engine.Group(prefix)
	}

	if m.own == nil {
		return m.Mount(group, app) //nolint:wrapcheck // the route's own message names the route
	}

	sec := *m.own
	if sec.Authorizer == nil {
		sec.Authorizer = app.Authorizer
	}

	return m.Mount(group, sec) //nolint:wrapcheck // the route's own message names the route
}

// Served lists the installed routes with the path each is served at (the prefix included, or the site root for a
// Root route), in the order they were collected, which is the order they are mounted.
func (a *App) Served() []route.Served {
	out := make([]route.Served, len(a.routes))
	for i, r := range a.routes {
		out[i] = route.Served{Spec: r.Spec(), Path: r.path(a.prefix)}
	}

	return out
}

// Contracts lists the groups the installed routes belong to, each once, in the order their routes were collected.
// A group none of whose routes is installed is not listed. It includes the groups of go-core's own modules (identity,
// access, notification...); the TypeScript generator leaves those out, because the frontend library ships their route
// tables (see Shipping).
func (a *App) Contracts() []*route.Contract { return route.Contracts(a.handled()...) }

func (a *App) handled() []route.Handled {
	out := make([]route.Handled, len(a.routes))
	for i, r := range a.routes {
		out[i] = r.Handled
	}

	return out
}

// found lists every problem so far: the ones features reported and the ones Env found.
func (a *App) found() []Problem {
	out := append([]Problem(nil), a.problems...)

	for _, p := range a.env.Problems() {
		out = append(out, Problem{What: p.What, Fix: p.Fix})
	}

	return out
}

// configure reads the configuration from the environment unless WithConfig gave one, adds the declared connections,
// and checks what it can without opening anything.
func (a *App) configure() {
	if !a.cfgSet {
		a.cfg = a.readConfig()
	}

	if !a.offline {
		a.cfg.Database.Connections = append(a.cfg.Database.Connections, a.readConnections()...)
	}

	envErr := a.cfg.App.Validate()
	if envErr != nil {
		a.fail(envErr.Error(), "set APP_ENV to production, staging or development (empty means development)")
	}

	err := gin.New().SetTrustedProxies(a.cfg.HTTP.TrustedProxies)
	if err != nil {
		a.fail("TRUSTED_PROXIES: "+err.Error(), "list IP addresses or CIDRs, comma-separated")
	}
}

// openLogger builds the logger from the log config, unless WithLogger gave one or the App is offline.
func (a *App) openLogger() {
	if a.log != nil {
		return
	}

	if a.offline {
		a.log = offlineLogger()

		return
	}

	log, err := logger.New(logger.Config{
		AppName:    a.cfg.App.Name,
		LogDir:     a.cfg.Log.Dir,
		Env:        a.cfg.App.Env,
		Level:      logger.LogLevel(a.cfg.Log.Level),
		MaxSizeMB:  a.cfg.Log.MaxSize,
		MaxBackups: a.cfg.Log.MaxBackups,
		MaxAgeDays: a.cfg.Log.MaxAgeDays,
	})
	if err != nil {
		a.fail("the logger could not be opened: "+err.Error(), "check the LOG_* settings, such as LOG_DIR being writable")

		log = slog.Default()
	}

	a.log = log
}

// openRegistry opens the configured connections, unless WithRegistry gave a registry or the App is offline.
func (a *App) openRegistry() {
	if a.registry != nil || a.offline || len(a.cfg.Database.Connections) == 0 {
		return
	}

	reg, err := openDatabase(a.log, a.cfg.Database)
	if err != nil {
		a.fail("the database could not be opened: "+err.Error(),
			"check the connection settings and that the server is reachable")
	}

	a.registry = reg
}
