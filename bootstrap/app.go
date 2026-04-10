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
	log := MustResolve[*slog.Logger](a.container)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Register Phase
	if err := a.registerModules(); err != nil {
		return err
	}

	// 2. Boot Phase
	if err := a.bootModules(ctx); err != nil {
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
			return fmt.Errorf("http server failed to start: %w", err)
		case <-time.After(100 * time.Millisecond):
		}
	}

	log.Info("application_running")
	if httpErrCh != nil {
		select {
		case <-ctx.Done():
		case err := <-httpErrCh:
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

// bootModules boots all modules concurrently using errgroup.
// If any module's Boot returns an error, the context is canceled and
// bootModules returns the first error encountered.
func (a *App) bootModules(ctx context.Context) error {
	g, gCtx := errgroup.WithContext(ctx)
	for _, m := range a.modules {
		g.Go(func() error {
			if err := m.Boot(gCtx); err != nil {
				return fmt.Errorf("module %q boot failed: %w", m.Name(), err)
			}
			return nil
		})
	}
	return g.Wait()
}

// shutdown gracefully shuts down all modules.
func (a *App) Shutdown(log *slog.Logger) {
	log.Info("shutting_down")

	timeout := a.cfg.HTTP.ShutdownTimeout
	if timeout == 0 {
		timeout = 10
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// Mark service as draining so readiness probes fail fast (zero-downtime friendly)
	if a.container != nil {

		if hr, err := Resolve[*health.HealthRegistry](a.container); err == nil {
			hr.SetDraining(true)
		}
	}

	// First, shutdown HTTP server to stop accepting new requests
	if a.httpServer != nil {
		if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("http_shutdown_failed", "error", err)
		}
	}

	// Shutdown modules in reverse order
	for i := len(a.modules) - 1; i >= 0; i-- {
		m := a.modules[i]
		if err := m.Shutdown(shutdownCtx); err != nil {
			log.Error("shutdown_failed", "module", m.Name(), "error", err)
		}
	}
}
