package product

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/audit"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	dbtypes "github.com/wssto2/go-core/database/types"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/observability"
	"github.com/wssto2/go-core/ratelimit"
	storagelocal "github.com/wssto2/go-core/storage/local"
	"github.com/wssto2/go-core/tenancy"
	"github.com/wssto2/go-core/worker"
	"gorm.io/gorm"
)

type Module struct {
	mgr          *worker.Manager
	log          *slog.Logger
	storageDir   string
	webhookURL   string
	webhookToken string
	catalogSvc   Service
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
	if err := seedDemoCatalog(db); err != nil {
		return fmt.Errorf("product: seed demo catalog: %w", err)
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
	m.catalogSvc = instrumentedSvc

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

// ListCatalog exposes a read-only product query that page composers can inject
// without reaching into the container.
func (m *Module) ListCatalog(ctx context.Context) ([]Product, error) {
	if m.catalogSvc == nil {
		return nil, fmt.Errorf("product: service not initialised")
	}
	return m.catalogSvc.List(ctx)
}

// GetCatalogProduct exposes a read-only single-product query for server-side
// page composition without leaking the service container into the SPA builder.
func (m *Module) GetCatalogProduct(ctx context.Context, id int) (Product, error) {
	if m.catalogSvc == nil {
		return Product{}, fmt.Errorf("product: service not initialised")
	}
	return m.catalogSvc.GetByID(ctx, id)
}

func seedDemoCatalog(db *gorm.DB) error {
	var count int64
	if err := db.Model(&Product{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	products := []Product{
		{
			Name:        "Aurora Desk Lamp",
			SKU:         "AUR-LAMP-001",
			Description: dbtypes.NewNullString("Minimal aluminum desk lamp with warm ambient light."),
			Price:       dbtypes.NewFloat(79.00),
			Stock:       18,
			Active:      dbtypes.NewBool(true),
			CreatedBy:   1,
		},
		{
			Name:        "Nimbus Travel Mug",
			SKU:         "NMB-MUG-002",
			Description: dbtypes.NewNullString("Insulated stainless steel mug designed for daily commutes."),
			Price:       dbtypes.NewFloat(24.50),
			Stock:       42,
			Active:      dbtypes.NewBool(true),
			CreatedBy:   1,
		},
		{
			Name:        "Atlas Notebook",
			SKU:         "ATL-NOTE-003",
			Description: dbtypes.NewNullString("Hardcover dotted notebook for product planning and sketches."),
			Price:       dbtypes.NewFloat(18.90),
			Stock:       9,
			Active:      dbtypes.NewBool(true),
			CreatedBy:   1,
		},
		{
			Name:        "Summit Carry Tote",
			SKU:         "SUM-TOTE-004",
			Description: dbtypes.NewNullString("Heavy canvas tote for laptops, cables, and day-trip essentials."),
			Price:       dbtypes.NewFloat(36.00),
			Stock:       0,
			Active:      dbtypes.NewBool(true),
			CreatedBy:   1,
		},
	}

	return db.Create(&products).Error
}
