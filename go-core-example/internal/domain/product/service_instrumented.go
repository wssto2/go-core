package product

import (
	"context"

	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/observability"
	"github.com/wssto2/go-core/resource"
)

// product/service_instrumented.go — generated or hand-written wrapper
// This is the only place observability touches the service layer.
type InstrumentedService struct {
	inner Service
	mw    *observability.ServiceObserver
}

func NewInstrumentedService(inner Service, mw *observability.ServiceObserver) *InstrumentedService {
	return &InstrumentedService{
		inner: inner,
		mw:    mw,
	}
}

func (s *InstrumentedService) GetDatatable(ctx context.Context, params datatable.QueryParams) (*datatable.DatatableResult[Product], error) {
	var result *datatable.DatatableResult[Product]
	err := s.mw.Do(ctx, "product", "GetDatatable", func(ctx context.Context) error {
		var err error
		result, err = s.inner.GetDatatable(ctx, params)
		return err
	})
	return result, err
}

func (s *InstrumentedService) GetResource(ctx context.Context, id int) (resource.Response[Product], error) {
	var result resource.Response[Product]
	err := s.mw.Do(ctx, "product", "GetResource", func(ctx context.Context) error {
		var err error
		result, err = s.inner.GetResource(ctx, id)
		return err
	})
	return result, err
}

func (s *InstrumentedService) List(ctx context.Context) ([]Product, error) {
	var result []Product
	err := s.mw.Do(ctx, "product", "List", func(ctx context.Context) error {
		var err error
		result, err = s.inner.List(ctx)
		return err
	})
	return result, err
}

func (s *InstrumentedService) GetByID(ctx context.Context, id int) (Product, error) {
	var result Product
	err := s.mw.Do(ctx, "product", "GetByID", func(ctx context.Context) error {
		var err error
		result, err = s.inner.GetByID(ctx, id)
		return err
	})
	return result, err
}

func (s *InstrumentedService) GetMany(ctx context.Context, ids []int) ([]Product, error) {
	var result []Product
	err := s.mw.Do(ctx, "product", "GetMany", func(ctx context.Context) error {
		var err error
		result, err = s.inner.GetMany(ctx, ids)
		return err
	})
	return result, err
}

func (s *InstrumentedService) Create(ctx context.Context, opts CreateProductOptions, actor auth.Identifiable) (*Product, error) {
	var result *Product
	err := s.mw.Do(ctx, "product", "Create", func(ctx context.Context) error {
		var err error
		result, err = s.inner.Create(ctx, opts, actor)
		return err
	})
	return result, err
}

func (s *InstrumentedService) Update(ctx context.Context, id int, opts UpdateProductOptions, actor auth.Identifiable) (*Product, error) {
	var result *Product
	err := s.mw.Do(ctx, "product", "Update", func(ctx context.Context) error {
		var err error
		result, err = s.inner.Update(ctx, id, opts, actor)
		return err
	})
	return result, err
}

func (s *InstrumentedService) Delete(ctx context.Context, id int, actor auth.Identifiable) error {
	err := s.mw.Do(ctx, "product", "Delete", func(ctx context.Context) error {
		var err error
		err = s.inner.Delete(ctx, id, actor)
		return err
	})
	return err
}
