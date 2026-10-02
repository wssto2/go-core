// Package bootstrap wires together the application lifecycle, dependency
// injection container, configuration loading, and HTTP server startup.
//
// Typical usage — create a Builder, register modules, and run:
//
//	app := bootstrap.NewBuilder(cfg).
//	    WithModule(mymodule.New()).
//	    WithJWTAuth(jwtCfg, resolver).
//	    Build()
//	app.Run()
//
// The DI container is accessible via bootstrap.Bind / bootstrap.Resolve during
// module registration so that packages can share services without explicit wiring.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/health"
	"golang.org/x/sync/errgroup"
)

// App is the main application instance that manages the lifecycle of modules.
type App struct {
	cfg        Config
	container  *Container
	engine     *gin.Engine
	httpServer HTTPServer
	modules    []Module
	booted     []bool             // booted[i]: modules[i].Boot returned nil; read after bootModules
	cancelBoot context.CancelFunc // ends the context Boot received; called at shutdown
}

// NewApp constructs an App instance.
func NewApp(cfg Config, container *Container, engine *gin.Engine, httpSrv HTTPServer, modules []Module) *App {
	return &App{
		cfg:        cfg,
		container:  container,
		engine:     engine,
		httpServer: httpSrv,
		modules:    modules,
	}
}

// Container exposes the container for post-Build bindings in tests.
// Production code should not call this after Build.
func (a *App) Container() *Container {
	return a.container
}

// Run starts the application and its modules, then waits for a termination signal.
func (a *App) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return a.RunContext(ctx)
}

// RunContext is Run that stops when ctx is done instead of on a signal. When
// start-up fails (a module's Boot, the HTTP listener), everything that already
// started is stopped again, in reverse order, before the error is returned.
func (a *App) RunContext(ctx context.Context) error {
	log := MustResolve[*slog.Logger](a.container)

	// 1. Register Phase
	if err := a.registerModules(); err != nil {
		return err
	}

	// 2. Boot Phase
	if err := a.bootModules(ctx); err != nil {
		a.shutdown(log, a.booted) //nolint:contextcheck // shutdown runs on a fresh timeout context: the run context is already done
		return err
	}

	var httpErrCh <-chan error
	// Start HTTP server if configured
	if a.httpServer != nil {
		errCh := make(chan error, 1)
		go func() {
			err := a.httpServer.Start()
			switch {
			case err == nil:
				errCh <- errors.New("http server exited unexpectedly")
			case !errors.Is(err, http.ErrServerClosed):
				errCh <- err
			}
		}()
		httpErrCh = errCh
		// Give the server a short window to detect immediate failures (e.g. port in use).
		select {
		case err := <-httpErrCh:
			a.Shutdown(log) //nolint:contextcheck // shutdown runs on a fresh timeout context: the run context is already done
			return fmt.Errorf("http server failed to start: %w", err)
		case <-time.After(100 * time.Millisecond):
		}
	}

	log.Info("application_running")
	if httpErrCh != nil {
		select {
		case <-ctx.Done():
		case err := <-httpErrCh:
			a.Shutdown(log) //nolint:contextcheck // shutdown runs on a fresh timeout context: the run context is already done
			return fmt.Errorf("http server stopped unexpectedly: %w", err)
		}
	} else {
		<-ctx.Done()
	}

	// 3. Shutdown Phase
	a.Shutdown(log)
	return nil
}

// registerModules calls Register on every module in declaration order.
// Register often mutates shared state such as the DI container or HTTP router,
// so keeping it sequential avoids racy startup behavior.
func (a *App) registerModules() error {
	for _, m := range a.modules {
		if err := m.Register(a.container); err != nil {
			return fmt.Errorf("module %q register failed: %w", m.Name(), err)
		}
	}
	return nil
}

// bootModules boots all modules concurrently. Boot receives a context that
// lives until shutdown, so a module may start workers on it. If any Boot
// returns an error, that context is canceled (siblings still booting see it)
// and bootModules returns the first error.
func (a *App) bootModules(ctx context.Context) error {
	// Not derived from the run context's cancellation: a signal must stop the
	// HTTP server first, and only then end what Boot started.
	bootCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a.cancelBoot = cancel

	var g errgroup.Group

	a.booted = make([]bool, len(a.modules))
	for i, m := range a.modules {
		g.Go(func() error {
			if err := m.Boot(bootCtx); err != nil {
				cancel()
				return fmt.Errorf("module %q boot failed: %w", m.Name(), err)
			}
			a.booted[i] = true
			return nil
		})
	}

	return g.Wait()
}

// Shutdown gracefully shuts down the HTTP server and all modules.
func (a *App) Shutdown(log *slog.Logger) {
	a.shutdown(log, nil)
}

// shutdown stops the HTTP server, then the modules in reverse order. With a
// non-nil only, just the modules marked true are stopped (those that booted).
func (a *App) shutdown(log *slog.Logger, only []bool) {
	log.Info("shutting_down")

	// ShutdownTimeout is a time.Duration (not seconds): multiplying it by
	// time.Second overflowed int64 for any timeout of 10 s or more and handed
	// the server and the modules an already expired context.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.HTTP.shutdownTimeout())
	defer cancel()

	// Mark service as draining so readiness probes fail fast (zero-downtime friendly)
	if a.container != nil {

		if hr, err := Resolve[*health.HealthRegistry](a.container); err == nil {
			hr.SetDraining(true)
		}
	}

	// First, shut down the HTTP server: stop accepting new requests, drain the
	// in-flight ones and, after the grace period, cancel the request contexts
	// that are still open (Server-Sent Events, long polling) so they end
	// instead of holding the shutdown for the whole timeout.
	if a.httpServer != nil {
		if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("http_shutdown_failed", "error", err)
		}
	}

	// Boot's context ends here: workers started on it begin to stop.
	if a.cancelBoot != nil {
		a.cancelBoot()
	}

	// Shutdown modules in reverse order
	for i := len(a.modules) - 1; i >= 0; i-- {
		if only != nil && !only[i] {
			continue
		}
		m := a.modules[i]
		if err := m.Shutdown(shutdownCtx); err != nil {
			log.Error("shutdown_failed", "module", m.Name(), "error", err)
		}
	}
}
