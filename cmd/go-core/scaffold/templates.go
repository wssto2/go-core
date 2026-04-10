package scaffold

// modelTpl generates the GORM model.
const modelTpl = `package {{.Package}}

import (
	"time"
	"gorm.io/gorm"
)

// {{.Pascal}} is the domain model and GORM schema for the {{.Package}} table.
type {{.Pascal}} struct {
	ID        int            ` + "`" + `gorm:"primaryKey;autoIncrement" json:"id"` + "`" + `
	Name      string         ` + "`" + `gorm:"not null"                json:"name"` + "`" + `
	CreatedAt time.Time      ` + "`" + `                              json:"created_at"` + "`" + `
	UpdatedAt *time.Time     ` + "`" + `                              json:"updated_at"` + "`" + `
	DeletedAt gorm.DeletedAt ` + "`" + `gorm:"index"                  json:"deleted_at"` + "`" + `
}

func ({{.Pascal}}) TableName() string { return "{{snake .Pascal}}s" }
`

// repositoryTpl generates the Repository interface + GORM implementation.
const repositoryTpl = `package {{.Package}}

import (
	"context"
	"errors"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/resource"
	"gorm.io/gorm"
)

// Repository defines the data-access contract for {{.Pascal}}.
// Use this interface in service tests to swap in a fake.
type Repository interface {
	FindByID(ctx context.Context, id int) ({{.Pascal}}, error)
	FindForResource(ctx context.Context, id int) (resource.Response[{{.Pascal}}], error)
	GetDatatable(ctx context.Context, params datatable.QueryParams) (*datatable.DatatableResult[{{.Pascal}}], error)
	Create(ctx context.Context, m *{{.Pascal}}) error
	Update(ctx context.Context, m *{{.Pascal}}) error
	Delete(ctx context.Context, id int) error
}

type repository struct {
	db *gorm.DB
}

// NewRepository constructs a GORM-backed Repository.
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) FindByID(ctx context.Context, id int) ({{.Pascal}}, error) {
	var m {{.Pascal}}
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return m, apperr.NotFound("{{.Package}} not found")
		}
		return m, apperr.Internal(err)
	}
	return m, nil
}

func (r *repository) FindForResource(ctx context.Context, id int) (resource.Response[{{.Pascal}}], error) {
	m, err := r.FindByID(ctx, id)
	if err != nil {
		return resource.Response[{{.Pascal}}]{}, err
	}
	return resource.Response[{{.Pascal}}]{Data: m}, nil
}

func (r *repository) GetDatatable(ctx context.Context, params datatable.QueryParams) (*datatable.DatatableResult[{{.Pascal}}], error) {
	return datatable.Paginate[{{.Pascal}}](r.db.WithContext(ctx), params)
}

func (r *repository) Create(ctx context.Context, m *{{.Pascal}}) error {
	return apperr.Internal(r.db.WithContext(ctx).Create(m).Error)
}

func (r *repository) Update(ctx context.Context, m *{{.Pascal}}) error {
	return apperr.Internal(r.db.WithContext(ctx).Save(m).Error)
}

func (r *repository) Delete(ctx context.Context, id int) error {
	return apperr.Internal(r.db.WithContext(ctx).Delete(&{{.Pascal}}{}, id).Error)
}
`

// serviceTpl generates the Service interface + implementation.
const serviceTpl = `package {{.Package}}

import (
	"context"
	"log/slog"

	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/resource"
	{{- if .Features.Audit}}
	"github.com/wssto2/go-core/audit"
	{{- end}}
)

// Service defines the business-logic contract for {{.Pascal}}.
type Service interface {
	GetDatatable(ctx context.Context, params datatable.QueryParams) (*datatable.DatatableResult[{{.Pascal}}], error)
	GetResource(ctx context.Context, id int) (resource.Response[{{.Pascal}}], error)
	GetByID(ctx context.Context, id int) ({{.Pascal}}, error)
	Create(ctx context.Context, opts Create{{.Pascal}}Options, actor auth.Identifiable) (*{{.Pascal}}, error)
	Update(ctx context.Context, id int, opts Update{{.Pascal}}Options, actor auth.Identifiable) (*{{.Pascal}}, error)
	Delete(ctx context.Context, id int, actor auth.Identifiable) error
}

type service struct {
	repo   Repository
	log    *slog.Logger
	{{- if .Features.Audit}}
	audit  audit.Repository
	{{- end}}
}

// NewService constructs the {{.Package}} service.
func NewService(repo Repository, log *slog.Logger{{if .Features.Audit}}, auditRepo audit.Repository{{end}}) Service {
	return &service{
		repo:  repo,
		log:   log,
		{{- if .Features.Audit}}
		audit: auditRepo,
		{{- end}}
	}
}

func (s *service) GetDatatable(ctx context.Context, params datatable.QueryParams) (*datatable.DatatableResult[{{.Pascal}}], error) {
	return s.repo.GetDatatable(ctx, params)
}

func (s *service) GetResource(ctx context.Context, id int) (resource.Response[{{.Pascal}}], error) {
	return s.repo.FindForResource(ctx, id)
}

func (s *service) GetByID(ctx context.Context, id int) ({{.Pascal}}, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *service) Create(ctx context.Context, opts Create{{.Pascal}}Options, actor auth.Identifiable) (*{{.Pascal}}, error) {
	m := &{{.Pascal}}{
		Name: opts.Name,
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}
	{{- if .Features.Audit}}
	_ = s.audit.Log(ctx, audit.Entry{Action: "{{.Package}}.create", ActorID: actor.GetID(), ResourceID: m.ID})
	{{- end}}
	return m, nil
}

func (s *service) Update(ctx context.Context, id int, opts Update{{.Pascal}}Options, actor auth.Identifiable) (*{{.Pascal}}, error) {
	m, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if opts.Name != "" {
		m.Name = opts.Name
	}
	if err := s.repo.Update(ctx, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *service) Delete(ctx context.Context, id int, actor auth.Identifiable) error {
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

// Create{{.Pascal}}Options holds the input for creating a {{.Pascal}}.
type Create{{.Pascal}}Options struct {
	Name string
}

// Update{{.Pascal}}Options holds the input for updating a {{.Pascal}}.
type Update{{.Pascal}}Options struct {
	Name string
}

// Sentinel so the compiler catches a missing service method.
var _ Service = (*service)(nil)
`

// handlerTpl generates the HTTP handler.
const handlerTpl = `package {{.Package}}

import (
	"net/http"

	"github.com/gin-gonic/gin"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/web"
)

type handler struct {
	service Service
}

func newHandler(svc Service) *handler {
	return &handler{service: svc}
}

func (h *handler) registerRoutes(rg *gin.RouterGroup) {
	rg.GET("", h.list)
	rg.GET("/:id", h.show)
	rg.POST("",
		coreauth.Authorize(coreauth.GeneratePolicy("{{.Package}}s", "create")),
		middlewares.BindRequest[Create{{.Pascal}}Request](),
		h.create,
	)
	rg.PUT("/:id",
		coreauth.Authorize(coreauth.GeneratePolicy("{{.Package}}s", "update")),
		middlewares.BindRequest[Update{{.Pascal}}Request](),
		h.update,
	)
	rg.DELETE("/:id",
		coreauth.Authorize(coreauth.GeneratePolicy("{{.Package}}s", "delete")),
		h.delete,
	)
}

func (h *handler) list(ctx *gin.Context) {
	params := datatable.ParamsFromGin(ctx)
	result, err := h.service.GetDatatable(ctx.Request.Context(), params)
	if err != nil {
		web.Fail(ctx, err)
		return
	}
	web.Paginated(ctx, result)
}

func (h *handler) show(ctx *gin.Context) {
	id, ok := web.GetPathID(ctx)
	if !ok {
		return
	}
	resp, err := h.service.GetResource(ctx.Request.Context(), id)
	if err != nil {
		web.Fail(ctx, err)
		return
	}
	web.JSON(ctx, http.StatusOK, resp.Data, resp.Meta)
}

func (h *handler) create(ctx *gin.Context) {
	req := middlewares.MustGetRequest[Create{{.Pascal}}Request](ctx)
	actor := coreauth.MustGetUser[coreauth.DefaultUser](ctx)
	created, err := h.service.Create(ctx.Request.Context(), req.ToInput(), actor)
	web.HandleCreate(ctx, created, err)
}

func (h *handler) update(ctx *gin.Context) {
	id, ok := web.GetPathID(ctx)
	if !ok {
		return
	}
	req := middlewares.MustGetRequest[Update{{.Pascal}}Request](ctx)
	actor := coreauth.MustGetUser[coreauth.DefaultUser](ctx)
	updated, err := h.service.Update(ctx.Request.Context(), id, req.ToInput(), actor)
	web.Handle(ctx, updated, err)
}

func (h *handler) delete(ctx *gin.Context) {
	id, ok := web.GetPathID(ctx)
	if !ok {
		return
	}
	actor := coreauth.MustGetUser[coreauth.DefaultUser](ctx)
	if err := h.service.Delete(ctx.Request.Context(), id, actor); err != nil {
		web.Fail(ctx, err)
		return
	}
	web.NoContent(ctx)
}
`

// requestsTpl generates Create/Update request structs.
const requestsTpl = `package {{.Package}}

// Create{{.Pascal}}Request is the parsed and validated HTTP input for creating a {{.Pascal}}.
type Create{{.Pascal}}Request struct {
	Name string ` + "`" + `form:"name" json:"name" validation:"required|min:2|max:255"` + "`" + `
}

func (r Create{{.Pascal}}Request) ToInput() Create{{.Pascal}}Options {
	return Create{{.Pascal}}Options{Name: r.Name}
}

// Update{{.Pascal}}Request allows partial updates — absent fields are ignored.
type Update{{.Pascal}}Request struct {
	Name string ` + "`" + `form:"name" json:"name" validation:"min:2|max:255"` + "`" + `
}

func (r Update{{.Pascal}}Request) ToInput() Update{{.Pascal}}Options {
	return Update{{.Pascal}}Options{Name: r.Name}
}
`

// moduleTpl generates the Module (DI wiring).
const moduleTpl = `package {{.Package}}

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	{{- if .Features.Audit}}
	"github.com/wssto2/go-core/audit"
	{{- end}}
	{{- if .Features.Events}}
	"github.com/wssto2/go-core/event"
	{{- end}}
	{{- if .Features.Worker}}
	"github.com/wssto2/go-core/worker"
	{{- end}}
	"log/slog"
)

// Module wires the {{.Pascal}} domain into the application.
// Register adds all dependencies and routes; Boot starts background workers.
type Module struct {
	log *slog.Logger
	{{- if .Features.Worker}}
	mgr *worker.Manager
	{{- end}}
}

func NewModule() *Module { return &Module{} }

func (m *Module) Name() string { return "{{.Package}}" }

func (m *Module) Register(c *bootstrap.Container) error {
	m.log = bootstrap.MustResolve[*slog.Logger](c)
	db := bootstrap.MustResolve[*database.Registry](c).Primary()

	if err := database.SafeMigrate(db, &{{.Pascal}}{}{{if .Features.Events}}, &event.OutboxEvent{}{{end}}); err != nil {
		return fmt.Errorf("{{.Package}}: migrate: %w", err)
	}

	{{- if .Features.Audit}}
	auditRepo := bootstrap.MustResolve[audit.Repository](c)
	{{- end}}

	repo := NewRepository(db)
	svc := NewService(repo, m.log{{if .Features.Audit}}, auditRepo{{end}})

	h := newHandler(svc)

	eng, err := bootstrap.Resolve[*gin.Engine](c)
	if err != nil {
		return fmt.Errorf("{{.Package}}: resolve engine: %w", err)
	}
	authProvider := bootstrap.MustResolve[coreauth.Provider](c)
	api := eng.Group("/api/v1")
	protected := api.Group("")
	protected.Use(coreauth.Authenticated(authProvider))
	h.registerRoutes(protected.Group("/{{.Package}}s"))

	{{- if .Features.Worker}}
	m.mgr = worker.NewManager(m.log)
	// TODO: add workers via m.mgr.Add(...)
	{{- end}}

	return nil
}

func (m *Module) Boot(ctx context.Context) error {
	{{- if .Features.Worker}}
	m.mgr.Start(ctx)
	{{- end}}
	return nil
}

func (m *Module) Shutdown(ctx context.Context) error {
	{{- if .Features.Worker}}
	if m.mgr != nil {
		done := make(chan struct{})
		go func() { m.mgr.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			return fmt.Errorf("{{.Package}}: shutdown timed out: %w", ctx.Err())
		}
	}
	{{- end}}
	return nil
}
`

// eventsTpl generates domain event structs.
const eventsTpl = `package {{.Package}}

import "github.com/wssto2/go-core/event"

// {{.Pascal}}CreatedEvent is emitted after a {{.Pascal}} is successfully created.
type {{.Pascal}}CreatedEvent struct {
	{{.Pascal}}ID int ` + "`" + `json:"{{snake .Pascal}}_id"` + "`" + `
}

func ({{.Pascal}}CreatedEvent) EventName() string { return "{{.Package}}.created" }

// Compile-time check.
var _ event.Event = {{.Pascal}}CreatedEvent{}
`

// workerTpl generates a background Worker stub.
const workerTpl = `package {{.Package}}

import (
	"context"
	"log/slog"
)

// {{.Pascal}}Worker processes background jobs for the {{.Package}} domain.
type {{.Pascal}}Worker struct {
	log *slog.Logger
}

func New{{.Pascal}}Worker(log *slog.Logger) *{{.Pascal}}Worker {
	return &{{.Pascal}}Worker{log: log}
}

func (w *{{.Pascal}}Worker) Name() string { return "{{snake .Pascal}}_worker" }

func (w *{{.Pascal}}Worker) Run(ctx context.Context) error {
	// TODO: implement worker logic
	<-ctx.Done()
	return nil
}
`

// migrationTpl generates a GORM AutoMigrate stub.
const migrationTpl = `package migrations

import (
	"fmt"
	"gorm.io/gorm"
)

// Migrate{{.Pascal}} applies the {{.Name}} schema change.
func Migrate{{.Pascal}}(db *gorm.DB) error {
	// TODO: implement migration
	// Example: return db.AutoMigrate(&MyModel{})
	if err := db.Exec("-- {{.Name}} migration").Error; err != nil {
		return fmt.Errorf("migrate {{.Name}}: %w", err)
	}
	return nil
}
`

// eventTpl generates a standalone event file (for go-core new event).
const eventTpl = `package {{.Package}}

import "github.com/wssto2/go-core/event"

// {{.Pascal}}Event carries the payload for the {{.Name}} domain event.
type {{.Pascal}}Event struct {
	// TODO: add event fields
}

func ({{.Pascal}}Event) EventName() string { return "{{.Package}}.{{actionName .Package .Pascal}}" }

// Compile-time check.
var _ event.Event = {{.Pascal}}Event{}
`
