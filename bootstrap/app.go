package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/logger"
	"gorm.io/gorm"
)

// App defines the entry point for an enterprise Go application.
type App struct {
	Config Config
	Router *gin.Engine
	Server *http.Server

	// Closers holds functions to be called during shutdown (e.g. closing DB, Redis)
	closers []func(ctx context.Context) error
}

type Migrator interface {
	Migrate(db *gorm.DB) error
}

// NewApp creates a new application instance.
func NewApp(cfg Config, router *gin.Engine) *App {
	return &App{
		Config: cfg,
		Router: router,
	}
}

// AddCloser registers a function to be called during graceful shutdown.
func (a *App) AddCloser(fn func(ctx context.Context) error) {
	a.closers = append(a.closers, fn)
}

// Run starts the HTTP server and blocks until an OS interrupt signal is received.
func (a *App) Run() error {
	a.Server = &http.Server{
		Addr:         fmt.Sprintf(":%d", a.Config.Port),
		Handler:      a.Router,
		ReadTimeout:  time.Duration(a.Config.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(a.Config.WriteTimeoutSec) * time.Second,
		IdleTimeout:  time.Duration(a.Config.IdleTimeoutSec) * time.Second,
	}

	serverErr := make(chan error, 1)

	// Start server in background
	go func() {
		logger.Log.Info("starting server", "port", a.Config.Port, "env", a.Config.Env)
		if err := a.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for termination signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		logger.Log.Error("server failed to start", "error", err)
		_ = a.Shutdown()
		return err
	case <-stop:
		return a.Shutdown()
	}
}

// Shutdown performs a graceful cleanup of all resources.
func (a *App) Shutdown() error {
	logger.Log.Info("shutting down gracefully...")

	// Default 10s timeout for cleanup
	timeout := a.Config.ShutdownTimeoutSec
	if timeout == 0 {
		timeout = 10
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// 1. Stop accepting new HTTP requests
	if err := a.Server.Shutdown(ctx); err != nil {
		logger.Log.Error("http shutdown error", "error", err)
	}

	// 2. Call all registered closers (DB, Cache, etc.) in reverse order
	for i := len(a.closers) - 1; i >= 0; i-- {
		if err := a.closers[i](ctx); err != nil {
			logger.Log.Error("cleanup error", "error", err)
		}
	}

	logger.Log.Info("shutdown complete")
	return nil
}

func (a *App) RegisterModules(
	engine *gin.Engine,
	c *Container,
	apiPrefix string,
	protectedMiddleware []gin.HandlerFunc,
	modules []Module,
) {
	// Health check -- outside the versioned API, no auth required.
	engine.GET("/health", HealthHandler(
		NewDBHealthChecker(c.PrimaryDB()),
	))

	v1 := engine.Group(apiPrefix)
	public := v1.Group("")

	protected := v1.Group("")
	protected.Use(protectedMiddleware...)

	for _, m := range modules {
		if mg, ok := m.(Migrator); ok {
			if err := mg.Migrate(c.PrimaryDB()); err != nil {
				panic("migration failed: " + err.Error())
			}
		}

		m.Register(c, public, protected)
	}
}
