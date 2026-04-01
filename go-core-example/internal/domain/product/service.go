package product

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/types"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/resource"
	"gorm.io/gorm"
)

// Service defines the business-logic contract for products.
// Handlers call the service; the service calls the repository and other
// infrastructure (audit log, event bus). Nothing in this layer touches gin.
//
//go:generate gowrap gen -p . -i ProductService -t observability -o service_instrumented.go
type Service interface {
	GetDatatable(ctx context.Context, params datatable.QueryParams) (*datatable.DatatableResult[Product], error)
	GetResource(ctx context.Context, id int) (resource.Response[Product], error)
	List(ctx context.Context) ([]Product, error)
	GetByID(ctx context.Context, id int) (Product, error)
	GetMany(ctx context.Context, ids []int) ([]Product, error)
	Create(ctx context.Context, opts CreateProductOptions, actor auth.Identifiable) (*Product, error)
	Update(ctx context.Context, id int, opts UpdateProductOptions, actor auth.Identifiable) (*Product, error)
	Delete(ctx context.Context, id int, actor auth.Identifiable) error
}

type service struct {
	repo       Repository
	transactor database.Transactor
	auditRepo  audit.Repository
	bus        event.Bus
	log        *slog.Logger
}

// NewService constructs the product service.
// All dependencies are injected — no globals, no package-level singletons.
func NewService(repo Repository, transactor database.Transactor, auditRepo audit.Repository, bus event.Bus, log *slog.Logger) Service {
	return &service{
		repo:       repo,
		transactor: transactor,
		auditRepo:  auditRepo,
		bus:        bus,
		log:        log,
	}
}

func (s *service) GetDatatable(ctx context.Context, params datatable.QueryParams) (*datatable.DatatableResult[Product], error) {
	return s.repo.GetDatatable(ctx, params)
}

func (s *service) GetResource(ctx context.Context, id int) (resource.Response[Product], error) {
	return s.repo.FindForResource(ctx, id)
}

func (s *service) List(ctx context.Context) ([]Product, error) {
	return s.repo.FindAll(ctx)
}

func (s *service) GetByID(ctx context.Context, id int) (Product, error) {
	product, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Product{}, apperr.NotFound("product not found")
		}
		return Product{}, apperr.Internal(err)
	}
	return *product, nil
}

func (s *service) GetMany(ctx context.Context, ids []int) ([]Product, error) {
	products, err := s.repo.FindByIDs(ctx, ids)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return products, nil
}

func (s *service) Create(ctx context.Context, opts CreateProductOptions, actor auth.Identifiable) (*Product, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	// Business rule: SKU must be unique across all non-deleted products.
	exists, err := s.repo.ExistsBySKU(ctx, opts.SKU, 0)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if exists {
		return nil, apperr.New(nil, "a product with this SKU already exists", apperr.CodeAlreadyExists).
			WithLog(apperr.LevelWarn)
	}

	product := &Product{
		Name:        opts.Name,
		SKU:         opts.SKU,
		Description: types.NewNullString(opts.Description),
		Price:       types.NewFloat(opts.Price),
		Stock:       opts.Stock,
		Active:      types.NewBool(false), // new products start inactive
		CreatedBy:   actor.GetID(),
		CreatedAt:   time.Now(),
	}

	if opts.CategoryID > 0 {
		product.CategoryID = types.NewNullInt(opts.CategoryID)
	}

	// Wrap create + audit in a single transaction using go-core's Transactor.
	// If either the INSERT or the audit write fails, both are rolled back.
	var created Product
	err = s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.Create(txCtx, product); err != nil {
			return apperr.Internal(err)
		}
		created = *product

		return s.auditRepo.Write(txCtx, audit.NewEntry("products", product.ID, actor.GetID(), "create").WithAfter(product))
	})
	if err != nil {
		return nil, err
	}

	if err := s.bus.Publish(ctx, ProductCreatedEvent{
		ProductID: product.ID,
		SKU:       product.SKU,
	}); err != nil {
		s.log.WarnContext(ctx, "failed to publish ProductCreatedEvent", "error", err)
	}

	return &created, nil
}

func (s *service) Update(ctx context.Context, id int, opts UpdateProductOptions, actor auth.Identifiable) (*Product, error) {

	if err := opts.Validate(); err != nil {
		return nil, err
	}

	product, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("product not found")
		}
		return nil, apperr.Internal(err)
	}

	// Business rule: if SKU is being changed, it must still be unique.
	if opts.SKU != "" && opts.SKU != product.SKU {
		exists, err := s.repo.ExistsBySKU(ctx, opts.SKU, id)
		if err != nil {
			return nil, apperr.Internal(err)
		}
		if exists {
			return nil, apperr.New(nil, "a product with this SKU already exists", apperr.CodeAlreadyExists).
				WithLog(apperr.LevelWarn)
		}
		product.SKU = opts.SKU
	}

	// Capture before-state for the audit diff.
	before := *product

	if opts.Name != "" {
		product.Name = opts.Name
	}
	if opts.Description != "" {
		product.Description = types.NewNullString(opts.Description)
	}
	if opts.Price > 0 {
		product.Price = types.NewFloat(opts.Price)
	}
	if opts.Stock >= 0 {
		product.Stock = opts.Stock
	}
	if opts.CategoryID > 0 {
		product.CategoryID = types.NewNullInt(opts.CategoryID)
	}

	now := time.Now()
	product.UpdatedBy = types.NewNullInt(actor.GetID())
	product.UpdatedAt = types.NewNullDateTime(now)

	changedFields := audit.Diff(before, *product)

	var updated Product
	err = s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.Update(txCtx, product); err != nil {
			return apperr.Internal(err)
		}
		updated = *product

		return s.auditRepo.Write(txCtx, audit.NewEntry("products", product.ID, actor.GetID(), "update").WithBefore(before).WithAfter(product).WithDiff(changedFields))
	})
	if err != nil {
		return nil, err
	}

	return &updated, nil
}

func (s *service) Delete(ctx context.Context, id int, actor auth.Identifiable) error {
	product, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return apperr.NotFound("product not found")
	}

	return s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.SoftDelete(txCtx, id, actor.GetID()); err != nil {
			return apperr.Internal(err)
		}

		return s.auditRepo.Write(txCtx, audit.NewEntry("products", product.ID, actor.GetID(), "delete").WithBefore(product))
	})
}
