package product

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/types"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/logger"
	"gorm.io/gorm"
)

// Service defines the business-logic contract for products.
// Handlers call the service; the service calls the repository and other
// infrastructure (audit log, event bus). Nothing in this layer touches gin.
type Service interface {
	List(ctx context.Context) ([]Product, error)
	GetByID(ctx context.Context, id int) (Product, error)
	GetMany(ctx context.Context, ids []int) ([]Product, error)
	Create(ctx context.Context, req CreateProductRequest, actor auth.Identifiable) (*Product, error)
	Update(ctx context.Context, id int, req UpdateProductRequest, actor auth.Identifiable) (*Product, error)
	Delete(ctx context.Context, id int, actor auth.Identifiable) error
}

type service struct {
	repo       Repository
	transactor database.Transactor
	auditRepo  audit.Repository
	bus        event.Bus
}

// NewService constructs the product service.
// All dependencies are injected — no globals, no package-level singletons.
func NewService(repo Repository, transactor database.Transactor, auditRepo audit.Repository, bus event.Bus) Service {
	return &service{
		repo:       repo,
		transactor: transactor,
		auditRepo:  auditRepo,
		bus:        bus,
	}
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

func (s *service) Create(ctx context.Context, req CreateProductRequest, actor auth.Identifiable) (*Product, error) {
	// Business rule: SKU must be unique across all non-deleted products.
	exists, err := s.repo.ExistsBySKU(ctx, req.SKU, 0)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if exists {
		return nil, apperr.New(nil, "a product with this SKU already exists", http.StatusConflict).
			WithLog(apperr.LevelWarn)
	}

	product := &Product{
		Name:        req.Name,
		SKU:         req.SKU,
		Description: types.NewNullString(req.Description),
		Price:       types.NewFloat(req.Price),
		Stock:       req.Stock,
		Active:      types.NewBool(false), // new products start inactive
		CreatedBy:   actor.GetID(),
		CreatedAt:   time.Now(),
	}

	if req.CategoryID > 0 {
		product.CategoryID = types.NewNullInt(req.CategoryID)
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
		logger.Log.WarnContext(ctx, "failed to publish ProductCreatedEvent", "error", err)
	}

	return &created, nil
}

func (s *service) Update(ctx context.Context, id int, req UpdateProductRequest, actor auth.Identifiable) (*Product, error) {
	product, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, apperr.NotFound("product not found")
	}

	// Business rule: if SKU is being changed, it must still be unique.
	if req.SKU != "" && req.SKU != product.SKU {
		exists, err := s.repo.ExistsBySKU(ctx, req.SKU, id)
		if err != nil {
			return nil, apperr.Internal(err)
		}
		if exists {
			return nil, apperr.New(nil, "a product with this SKU already exists", http.StatusConflict).
				WithLog(apperr.LevelWarn)
		}
		product.SKU = req.SKU
	}

	// Capture before-state for the audit diff.
	before := *product

	if req.Name != "" {
		product.Name = req.Name
	}
	if req.Description != "" {
		product.Description = types.NewNullString(req.Description)
	}
	if req.Price > 0 {
		product.Price = types.NewFloat(req.Price)
	}
	if req.Stock >= 0 {
		product.Stock = req.Stock
	}
	if req.CategoryID > 0 {
		product.CategoryID = types.NewNullInt(req.CategoryID)
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
