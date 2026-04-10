package product

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/observability"
	"github.com/wssto2/go-core/ratelimit"
	storagelocal "github.com/wssto2/go-core/storage/local"
	"github.com/wssto2/go-core/tenancy"
	"github.com/wssto2/go-core/worker"
)

type Module struct {
	mgr          *worker.Manager
	log          *slog.Logger
	storageDir   string
	webhookURL   string
	webhookToken string
}

func NewModule(storageDir, webhookURL, webhookToken string) *Module {
	return &Module{storageDir: storageDir, webhookURL: webhookURL, webhookToken: webhookToken}
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

	// Run AutoMigrate to create/update the products, audit_logs, and outbox_events tables.
	if err := database.SafeMigrate(db, &Product{}, &audit.AuditLog{}, &event.OutboxEvent{}); err != nil {
		return fmt.Errorf("product: migrate: %w", err)
	}

	bus := bootstrap.MustResolve[event.Bus](c)
	log := bootstrap.MustResolve[*slog.Logger](c)
	auditRepo := bootstrap.MustResolve[audit.Repository](c)

	// Local filesystem storage for product images. Swap for S3/GCS driver here.
	store, err := storagelocal.New(m.storageDir)
	if err != nil {
		return fmt.Errorf("product: storage: %w", err)
	}

	// Construct repo, service and HTTP handlers.
	repo := NewRepository(db)
	svc := NewService(repo, tx, auditRepo, store, log)
	instrumentedSvc := NewInstrumentedService(svc, tel.Service)

	// Idempotency store deduplicates POST /products retries for 24 hours.
	idempotencyStore := middlewares.NewInMemoryIdempotencyStore(24 * time.Hour)
	h := newHandler(instrumentedSvc, idempotencyStore)

	// Attach routes under /api/v1/products
	eng, err := bootstrap.Resolve[*gin.Engine](c)
	if err != nil {
		return fmt.Errorf("product: resolve engine: %w", err)
	}

	authProvider := bootstrap.MustResolve[coreauth.Provider](c)

	// Per-user, per-endpoint rate limiting (100 req/min).
	limiter := ratelimit.NewInMemoryLimiter(100, time.Minute)

	api := eng.Group("/api/v1")
	protected := api.Group("")
	protected.Use(coreauth.Authenticated(authProvider))
	protected.Use(tenancy.FromAuthenticatedUser())
	protected.Use(middlewares.LoadShedding(runtime.NumCPU()*4, 0))
	protected.Use(middlewares.RateLimit(limiter, true, true))
	h.registerRoutes(protected.Group("/products"))

	m.mgr = worker.NewManager(m.log, worker.WithManagerMetrics(tel.Worker))

	// Image processing worker — generates thumbnails/variants after upload.
	imgWorker, err := newImageWorker(repo, store, m.log)
	if err != nil {
		return fmt.Errorf("product: init image worker: %w", err)
	}
	if err := imgWorker.Subscribe(bus); err != nil {
		return fmt.Errorf("product: image worker subscribe: %w", err)
	}
	m.mgr.Add(imgWorker)

	// Outbox worker — durable external delivery to webhook, also replays
	// ProductImageUploadedEvent for crash recovery so imgWorker gets another chance.
	outboxWorker := event.NewOutboxWorker(
		db,
		NewWebhookPublisher(m.webhookURL, m.webhookToken, bus),
		m.log,
		500*time.Millisecond,
		50,
	)
	m.mgr.Add(outboxWorker)

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
