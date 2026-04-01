package product

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/observability"
	"github.com/wssto2/go-core/tenancy"
	"github.com/wssto2/go-core/worker"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Module struct {
	mgr *worker.Manager
	log *slog.Logger
}

func NewModule() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return "product"
}

// Register wires domain services and routes into the application's container
// and HTTP engine. It intentionally keeps side-effects here so Boot can start
// long-running background workers using the stored container reference.
func (m *Module) Register(c *bootstrap.Container) error {
	m.log = bootstrap.MustResolve[*slog.Logger](c)
	tel := bootstrap.MustResolve[*observability.Telemetry](c)

	db := bootstrap.MustResolve[*database.Registry](c).Primary()
	tx := database.NewTransactor(db)
	bus := bootstrap.MustResolve[event.Bus](c)
	log := bootstrap.MustResolve[*slog.Logger](c)
	auditRepo := bootstrap.MustResolve[audit.Repository](c)
	mw := bootstrap.MustResolve[*observability.ServiceObserver](c)

	// Construct repo, service and HTTP handlers
	repo := NewRepository(db)
	svc := NewService(repo, tx, auditRepo, bus, log)
	instrumentedSvc := NewInstrumentedService(svc, mw)
	h := newHandler(instrumentedSvc)

	// Attach routes under /api/v1/products
	eng, err := bootstrap.Resolve[*gin.Engine](c)
	if err != nil {
		return fmt.Errorf("product: resolve engine: %w", err)
	}

	// Expose /metrics endpoint
	eng.GET("/metrics", gin.WrapH(promhttp.Handler()))

	api := eng.Group("/api/v1")
	protected := api.Group("")
	// Demonstrate tenancy middleware which injects tenant information
	protected.Use(tenancy.FromAuthenticatedUser())
	h.registerRoutes(protected.Group("/products"))

	// Build worker — subscription happens once here, not inside Run
	w, err := NewProductWorker(bus, m.log)
	if err != nil {
		return fmt.Errorf("product: init worker: %w", err)
	}

	m.mgr = worker.NewManager(m.log, worker.WithManagerMetrics(tel.Worker))
	m.mgr.Add(w)

	return nil
}

// Boot starts background workers. Register stores the container so Boot can
// resolve services needed by long-running processes.
func (m *Module) Boot(ctx context.Context) error {
	m.mgr.Start(ctx)
	return nil
}

// Shutdown waits for workers to finish. The App will cancel the provided ctx
// which signals workers to stop.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.mgr == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		m.mgr.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("product: shutdown timed out: %w", ctx.Err())
	}
}
