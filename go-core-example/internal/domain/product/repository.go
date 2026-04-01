package product

import (
	"context"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/resource"
	"gorm.io/gorm"
)

// Repository defines the data-access contract for products.
// Handlers and services depend on this interface, never on the concrete type,
// making the implementation swappable for tests (SQLite in-memory) or caching layers.
type Repository interface {
	GetDatatable(ctx context.Context, params datatable.QueryParams) (*datatable.DatatableResult[Product], error)
	FindForResource(ctx context.Context, id int) (resource.Response[Product], error)
	FindAll(ctx context.Context) ([]Product, error)
	FindByID(ctx context.Context, id int) (*Product, error)
	FindByIDs(ctx context.Context, ids []int) ([]Product, error)
	FindBySKU(ctx context.Context, sku string) (*Product, error)
	Create(ctx context.Context, product *Product) error
	Update(ctx context.Context, product *Product) error
	SoftDelete(ctx context.Context, id int, deletedBy int) error
	ExistsBySKU(ctx context.Context, sku string, excludeID int) (bool, error)
}

// gormRepository is the MySQL/GORM-backed implementation of Repository.
// It is unexported — callers receive the Repository interface from NewRepository.
type gormRepository struct {
	conn *gorm.DB
}

// NewRepository constructs a Repository backed by the provided *gorm.DB.
// Inject the connection from the database.Registry, not a global.
func NewRepository(conn *gorm.DB) Repository {
	return &gormRepository{conn: conn}
}

// db returns the appropriate *gorm.DB for the given context. If a transaction
// is present in the context (via Transactor), that transaction is used.
func (r *gormRepository) db(ctx context.Context) *gorm.DB {
	if tx, ok := database.TxFromContext(ctx); ok {
		return tx.WithContext(ctx)
	}
	return r.conn.WithContext(ctx)
}

func (r *gormRepository) GetDatatable(ctx context.Context, params datatable.QueryParams) (*datatable.DatatableResult[Product], error) {
	return datatable.New[Product](r.db(ctx), params).
		WithColumns([]string{"id", "name", "sku", "price", "stock", "active", "created_at"}).
		WithSearchableFields([]string{"name", "sku"}).
		WithDefaultOrder("id", "desc").
		WithFilter(datatable.NewFilter("active", func(q *gorm.DB, val, table string) *gorm.DB {
			return q.Where(table+".active = ?", val)
		})).
		WithScope(func(q *gorm.DB, table string) *gorm.DB {
			return q.Where(table + ".deleted_at IS NULL")
		}).
		Get()
}

func (r *gormRepository) FindForResource(ctx context.Context, id int) (resource.Response[Product], error) {
	return resource.New[Product](r.db(ctx)).
		WithCount("audit_logs", "entity_id", "entity_type = 'products'").
		WithoutDeleted("deleted_at").
		FindByID(id)
}

func (r *gormRepository) FindAll(ctx context.Context) ([]Product, error) {
	var products []Product
	err := r.db(ctx).
		Where("deleted_at IS NULL").
		Order("id ASC").
		Find(&products).Error
	return products, err
}

func (r *gormRepository) FindByID(ctx context.Context, id int) (*Product, error) {
	var product Product
	err := r.db(ctx).
		Where("deleted_at IS NULL").
		First(&product, id).Error
	if err != nil {
		return nil, err
	}
	return &product, nil
}

func (r *gormRepository) FindByIDs(ctx context.Context, ids []int) ([]Product, error) {
	var products []Product
	err := r.db(ctx).
		Where("deleted_at IS NULL").
		Find(&products, ids).Error
	return products, err
}

func (r *gormRepository) FindBySKU(ctx context.Context, sku string) (*Product, error) {
	var product Product
	err := r.db(ctx).
		Where("sku = ? AND deleted_at IS NULL", sku).
		First(&product).Error
	if err != nil {
		return nil, err
	}
	return &product, nil
}

func (r *gormRepository) Create(ctx context.Context, product *Product) error {
	return r.db(ctx).Create(product).Error
}

func (r *gormRepository) Update(ctx context.Context, product *Product) error {
	// Save all fields — the caller is responsible for mutating the struct.
	return r.db(ctx).Save(product).Error
}

func (r *gormRepository) SoftDelete(ctx context.Context, id int, deletedBy int) error {
	// Soft delete: set deleted_at to now. The entity is still in the DB for audit trails.
	return r.db(ctx).
		Model(&Product{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"deleted_at": gorm.Expr("NOW()"),
			"updated_by": deletedBy,
		}).Error
}

func (r *gormRepository) ExistsBySKU(ctx context.Context, sku string, excludeID int) (bool, error) {
	var count int64
	query := r.db(ctx).Model(&Product{}).Where("sku = ? AND deleted_at IS NULL", sku)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}
