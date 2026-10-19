// Package bootstrap wires together the application lifecycle, configuration
// and HTTP server startup. Applications reach it through gocore, which builds
// the engine and runs the components it hands over.
package gocore

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

// component is a unit the App mounts on its engine, starts and stops. gocore
// supplies the one that installs routes and runs workers.
type component interface {
	Name() string
	// Register runs before Boot, with the engine every middleware is already on.
	Register(engine *gin.Engine) error
	Boot(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// lifecycle is the main application instance that manages the lifecycle of its components.
type lifecycle struct {
	cfg        Config
	log        *slog.Logger
	health     *health.HealthRegistry
	engine     *gin.Engine
	httpServer httpServer
	modules    []component
	booted     []bool             // booted[i]: modules[i].Boot returned nil; read after bootModules
	cancelBoot context.CancelFunc // ends the context Boot received; called at shutdown
}

// newLifecycle constructs an App instance.
func newLifecycle(
	cfg Config, log *slog.Logger, hr *health.HealthRegistry, engine *gin.Engine, httpSrv httpServer, modules []component,
) *lifecycle {
	lc := new(lifecycle)
	lc.cfg, lc.log, lc.health, lc.engine, lc.httpServer, lc.modules = cfg, log, hr, engine, httpSrv, modules

	return lc
}

// Run starts the application and its modules, then waits for a termination signal.
func (a *lifecycle) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return a.RunContext(ctx)
}

// RunContext is Run that stops when ctx is done instead of on a signal. When
// start-up fails (a module's Boot, the HTTP listener), everything that already
// started is stopped again, in reverse order, before the error is returned.
func (a *lifecycle) RunContext(ctx context.Context) error {
	log := a.log

	// 1. Register Phase
	err := a.registerModules()
	if err != nil {
		return err
	}

	// 2. Boot Phase
	err = a.bootModules(ctx)
	if err != nil {
		a.shutdown(log, a.booted) //nolint:contextcheck // shutdown runs on a fresh timeout context: the run context is already done

		return err
	}

	httpErrCh, err := a.startHTTP()
	if err != nil {
		a.Shutdown(log) //nolint:contextcheck // shutdown runs on a fresh timeout context: the run context is already done

		return err
	}

	log.Info("application_running")

	select {
	case <-ctx.Done():
	case err = <-httpErrCh:
		a.Shutdown(log) //nolint:contextcheck // shutdown runs on a fresh timeout context: the run context is already done

		return fmt.Errorf("http server stopped unexpectedly: %w", err)
	}

	// 3. Shutdown Phase
	a.Shutdown(log)

	return nil
}

// Shutdown gracefully shuts down the HTTP server and all modules.
func (a *lifecycle) Shutdown(log *slog.Logger) {
	a.shutdown(log, nil)
}

// shutdown stops the HTTP server, then the modules in reverse order. With a
// non-nil only, just the modules marked true are stopped (those that booted).
func (a *lifecycle) shutdown(log *slog.Logger, only []bool) {
	log.Info("shutting_down")

	// ShutdownTimeout is a time.Duration (not seconds): multiplying it by
	// time.Second overflowed int64 for any timeout of 10 s or more and handed
	// the server and the modules an already expired context.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.HTTP.shutdownTimeout())
	defer cancel()

	// Mark service as draining so readiness probes fail fast (zero-downtime friendly)
	if a.health != nil {
		a.health.SetDraining(true)
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

// registerModules calls Register on every component in declaration order.
// Register mounts routes on the shared engine, so keeping it sequential avoids
// racy startup behavior.
func (a *lifecycle) registerModules() error {
	for _, m := range a.modules {
		err := m.Register(a.engine)
		if err != nil {
			return fmt.Errorf("module %q register failed: %w", m.Name(), err)
		}
	}

	return nil
}

// bootModules boots all modules concurrently. Boot receives a context that
// lives until shutdown, so a module may start workers on it. If any Boot
// returns an error, that context is canceled (siblings still booting see it)
// and bootModules returns the first error.
func (a *lifecycle) bootModules(ctx context.Context) error {
	// Not derived from the run context's cancellation: a signal must stop the
	// HTTP server first, and only then end what Boot started.
	bootCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a.cancelBoot = cancel

	var group errgroup.Group

	a.booted = make([]bool, len(a.modules))

	for idx, mod := range a.modules {
		group.Go(func() error {
			err := mod.Boot(bootCtx)
			if err != nil {
				cancel()

				return fmt.Errorf("module %q boot failed: %w", mod.Name(), err)
			}

			a.booted[idx] = true

			return nil
		})
	}

	err := group.Wait()
	if err != nil {
		return fmt.Errorf("booting: %w", err)
	}

	return nil
}

var errServerExited = errors.New("http server exited unexpectedly")

// startupWindow is how long the HTTP server has to fail at once (a port in use) for the start to fail.
const startupWindow = 100 * time.Millisecond

// startHTTP starts the HTTP server, when there is one, and gives it a short window to fail at once. The channel
// carries a later failure; without a server it never delivers.
func (a *lifecycle) startHTTP() (<-chan error, error) {
	errCh := make(chan error, 1)

	if a.httpServer == nil {
		return errCh, nil
	}

	go func() {
		err := a.httpServer.Start()

		switch {
		case err == nil:
			errCh <- errServerExited
		case !errors.Is(err, http.ErrServerClosed):
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return nil, fmt.Errorf("http server failed to start: %w", err)
	case <-time.After(startupWindow):
		return errCh, nil
	}
}
