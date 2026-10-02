package gocore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/observability"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/worker"
)

// Check looks over the whole application and returns every problem found, not
// just the first, each with what to do about it. Run calls it first; call it
// yourself in a test to assert a wiring mistake is caught.
//
// It reports: a Later that was never set, a declared route without a handler,
// two routes on the same method and path, a route that Requires a permission
// missing from the catalogue, a route that is neither Public nor has
// authentication configured (or needs an authorizer and has none), and
// a database or connection that could not be resolved.
func (a *App) Check() error {
	return a.checked(nil)
}

// checked is Check plus the extra problems.
func (a *App) checked(extra []Problem) error {
	problems := append([]Problem(nil), a.problems...)
	problems = append(problems, extra...)

	for _, l := range a.laters {
		if !l.isSet() {
			problems = append(problems, Problem{
				What: "gocore.Later[" + l.typeName() + "] was never set",
				Fix:  "call Set on it once every feature that needs it is installed",
			})
		}
	}

	for _, spec := range route.Unhandled(a.routes...) {
		problems = append(problems, Problem{
			What: "route " + spec.String() + " is declared but has no handler",
			Fix:  "bind a handler with the route's To method and pass it to app.Routes in the feature's Install",
		})
	}

	problems = append(problems, a.routeProblems()...)

	if len(problems) == 0 {
		return nil
	}

	return &StartupError{Problems: problems}
}

func (a *App) routeProblems() []Problem {
	var problems []Problem

	seen := map[string]bool{}
	scratch := gin.New()
	duplicate := map[int]bool{}

	for i, r := range a.routes {
		spec := r.Spec()

		key := spec.Method + " " + spec.Path
		if seen[key] {
			problems = append(problems, Problem{
				What: "route " + key + " is installed twice",
				Fix:  "remove one of them, or give them different paths",
			})

			duplicate[i] = true
		}

		seen[key] = true

		if spec.Permission == "" {
			continue
		}

		if !a.catalogued(spec.Permission) {
			problems = append(problems, Problem{
				What: "route " + spec.String() + " requires " + spec.Permission + ", which no permission catalogue defines",
				Fix:  "define it in the catalogue and pass the catalogue to app.Permissions, or fix the id in Requires",
			})
		}
	}

	// A dry mount finds what only mounting can: an input type that cannot be
	// bound, or paths the router refuses (a clash of wildcards).
	for i, r := range a.routes {
		if duplicate[i] {
			continue
		}

		if err := a.dryMount(scratch, r); err != nil {
			problems = append(problems, Problem{What: err.Error(), Fix: "see the message: configure authentication or the authorizer, mark the route .Public(), declare a struct input (or route.None), or change a clashing path"})
		}
	}

	return problems
}

// dryMount mounts r on a scratch router, turning the router's panic on a
// path it refuses into an error.
func (a *App) dryMount(scratch *gin.Engine, r route.Handled) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("route %s was refused by the router: %v", r.Spec(), p)
		}
	}()

	return r.Mount(scratch, a.security())
}

func (a *App) catalogued(permission string) bool {
	for _, c := range a.catalogues {
		if _, ok := c.Lookup(permission); ok {
			return true
		}
	}

	return false
}

// Run is the application's whole command line. Without arguments it checks
// the application (including that no migration is pending), boots it, serves
// HTTP and waits for SIGINT or SIGTERM; then it shuts down: HTTP first, then
// the modules and background workers drain, and the database closes last. A
// failing check or boot returns an error with nothing left running.
//
// With a command it does that command instead, without serving or booting
// anything: migrate, migrate status, help. See RunCommand.
func (a *App) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return a.RunCommand(ctx, os.Args[1:], os.Stdout)
}

// RunContext serves like Run without arguments, and stops when ctx is done
// instead of on a signal.
func (a *App) RunContext(ctx context.Context) error {
	if a.started {
		return errors.New("gocore: Run was already called on this App")
	}

	a.started = true

	defer a.closeDatabase()

	// Migrations are checked only here: Check stays free of database access.
	var pending []Problem
	if a.registry != nil {
		pending = a.pendingProblems(ctx)
	}

	if err := a.checked(pending); err != nil {
		return err
	}

	booted, err := a.build() //nolint:contextcheck // the health handler the builder installs makes its own contexts
	if err != nil {
		return err
	}

	return booted.RunContext(ctx)
}

func (a *App) closeDatabase() {
	if a.registry == nil {
		return
	}

	if err := a.registry.CloseAll(); err != nil {
		a.log.Error("database_close_failed", "error", err)
	}
}

func (a *App) build() (*bootstrap.App, error) {
	b := bootstrap.New(a.cfg).WithLogger(a.log)
	if a.registry != nil {
		b.WithRegistry(a.registry)
	}

	b.DefaultInfrastructure()

	for _, configure := range a.customize {
		configure(b)
	}

	modules := append([]bootstrap.Module(nil), a.modules...)
	modules = append(modules, &featuresModule{app: a})

	booted, err := b.WithModules(modules...).WithHttp().Build()
	if err != nil {
		return nil, fmt.Errorf("gocore: %w", err)
	}

	return booted, nil
}

// featuresModule mounts the collected routes and runs the background workers.
// It is registered last, so it is shut down first: workers drain before the
// modules they may use.
type featuresModule struct {
	app    *App
	mgr    *worker.Manager
	cancel context.CancelFunc
}

func (m *featuresModule) Name() string { return "gocore" }

func (m *featuresModule) Register(c *bootstrap.Container) error {
	engine, err := bootstrap.Resolve[*gin.Engine](c)
	if err != nil {
		return fmt.Errorf("gocore: routes need the HTTP engine: %w", err)
	}

	for _, r := range m.app.routes {
		if err := r.Mount(engine, m.app.security()); err != nil {
			return err
		}
	}

	if len(m.app.workers) > 0 {
		var opts []worker.ManagerOption
		if tel, err := bootstrap.Resolve[*observability.Telemetry](c); err == nil {
			opts = append(opts, worker.WithManagerMetrics(tel.Worker))
		}

		m.mgr = worker.NewManager(m.app.log, opts...)
		m.mgr.Add(m.app.workers...)
	}

	return nil
}

// Boot starts the workers on a context that ends when Shutdown cancels it.
func (m *featuresModule) Boot(boot context.Context) error {
	if m.mgr == nil {
		return nil
	}

	ctx, cancel := context.WithCancel(boot)
	m.cancel = cancel
	m.mgr.Start(ctx)

	return nil
}

func (m *featuresModule) Shutdown(ctx context.Context) error {
	if m.mgr == nil || m.cancel == nil {
		return nil
	}

	m.cancel()

	done := make(chan struct{})

	go func() {
		m.mgr.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("gocore: background workers did not stop in time: %w", ctx.Err())
	}
}

// Handler returns the installed routes as an http.Handler without opening a
// port or booting modules, for tests. It checks the application first, like
// Run, and maps errors the way a running application does, but leaves out the
// rest of the middleware stack (request id, CORS, rate limits).
func (a *App) Handler() (http.Handler, error) {
	if err := a.Check(); err != nil {
		return nil, err
	}

	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(middlewares.ErrorHandler(a.log, nil, true))

	for _, r := range a.routes {
		if err := r.Mount(engine, a.security()); err != nil {
			return nil, err
		}
	}

	return engine, nil
}
