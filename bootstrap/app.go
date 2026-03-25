package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/auth"
)

// AppBuilder constructs an App using a fluent chain of calls.
//
// The builder is framework-agnostic. It does not import Gin, Chi, or any HTTP
// library. The HTTP adapter (e.g. adapters/coregin.GinAdapter) is injected via
// WithServer, which provides:
//   - An http.Handler for the *http.Server
//   - A Router implementation for public and protected route groups
//
// Typical usage:
//
//	adapter := coregin.New(coregin.Config{Env: cfg.Env, CORSOrigins: cfg.CORSOrigins})
//	adapter.RegisterHealth(bootstrap.NewDBHealthChecker(container.PrimaryDB()))
//
//	app, err := bootstrap.New[appauth.User](cfg).
//	    DefaultInfrastructure().
//	    WithServer(adapter).
//	    WithJWTAuth(appauth.IdentityResolver).
//	    Register(product.NewModule()).
//	    Build()
type App[T auth.Identifiable] struct {
	config    Config
	container *Container[T]
	server    *http.Server
	engine    *gin.Engine

	// Closers holds functions to be called during shutdown (e.g. closing DB, Redis)
	closers []func(ctx context.Context) error
}

// NewApp constructs an App. Prefer AppBuilder over calling this directly.
func NewApp[T auth.Identifiable](cfg Config, container *Container[T], handler *gin.Engine) *App[T] {
	readTimeout := durationOrDefault(cfg.ReadTimeoutSec, 15)
	writeTimeout := durationOrDefault(cfg.WriteTimeoutSec, 15)
	idleTimeout := durationOrDefault(cfg.IdleTimeoutSec, 60)
	port := cfg.Port
	if port == 0 {
		port = 8080
	}

	return &App[T]{
		config:    cfg,
		container: container,
		engine:    handler,
		server: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      handler,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
			IdleTimeout:  idleTimeout,
		},
	}
}

// AddCloser registers a cleanup function called on Shutdown.
// Closers are called in reverse registration order (LIFO).
func (a *App[T]) AddCloser(fn func(ctx context.Context) error) {
	a.closers = append(a.closers, fn)
}

// Run starts the HTTP server and blocks until an OS interrupt signal is received.
func (a *App[T]) Run() error {
	serverErr := make(chan error, 1)

	// Start server in background
	go func() {
		a.container.Log().Info("starting server",
			"port", a.config.Port,
			"env", a.config.Env,
		)

		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for termination signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		a.container.Log().Error("server failed to start", "error", err)
		_ = a.Shutdown()
		return err
	case <-stop:
		return a.Shutdown()
	}
}

// Shutdown performs a graceful cleanup of all resources.
func (a *App[T]) Shutdown() error {
	a.container.Log().Info("shutting down gracefully...")

	// Default 10s timeout for cleanup
	timeout := a.config.ShutdownTimeoutSec
	if timeout == 0 {
		timeout = 10
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var errs []error

	// 1. Stop accepting new HTTP requests
	if err := a.server.Shutdown(ctx); err != nil {
		a.container.Log().Error("http shutdown error", "error", err)
		errs = append(errs, fmt.Errorf("http shutdown: %w", err))
	}

	// 2. Call all registered closers (DB, Cache, etc.) in reverse order
	for i := len(a.closers) - 1; i >= 0; i-- {
		if err := a.closers[i](ctx); err != nil {
			a.container.Log().Error("cleanup error", "error", err)
			errs = append(errs, err)
		}
	}

	a.container.Log().Info("shutdown complete")
	return errors.Join(errs...)
}

// runMigrations calls Migrate on every module that implements Migrator.
// Returns an error if any migration fails; never panics.
func (a *App[T]) runMigrations(c *Container[T], modules []Module[T]) error {
	var errs []error
	for _, m := range modules {
		mg, ok := m.(Migrator)
		if !ok {
			continue
		}
		if err := mg.Migrate(c.PrimaryDB()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// registerModules calls Register on every module, passing the public and
// protected routers provided by the adapter.
func (a *App[T]) registerModules(c *Container[T], router *gin.Engine, modules []Module[T]) {
	public := router.Group("")
	protected := router.Group("/api")

	for _, m := range modules {
		m.Register(c, public, protected)
	}
}

func durationOrDefault(seconds, defaultSeconds int) time.Duration {
	if seconds == 0 {
		return time.Duration(defaultSeconds) * time.Second
	}
	return time.Duration(seconds) * time.Second
}
