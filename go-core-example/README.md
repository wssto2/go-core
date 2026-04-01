# go-core Example Application

A minimal but complete Go application demonstrating every feature of `go-core`.
Uses Gin + GORM + MySQL, structured around a single **Product** domain.

---

## Directory layout

```
example/
├── cmd/
│   └── api/
│       └── main.go          # Entrypoint — wires everything together
├── internal/
│   ├── domain/
│   │   └── product/
│   │       ├── entity.go    # GORM model + request structs
│   │       ├── repository.go # Repository interface + GORM implementation
│   │       └── service.go   # Business logic (uses audit, transactor)
│   ├── http/
│   │   └── handler/
│   │       └── product.go   # Gin handlers (datatable, resource, guards)
│   └── middleware/
│       └── resolver.go      # auth.Resolver — loads user from DB after JWT
├── migrations/
│   └── 001_create_products.sql
├── i18n/
│   ├── en.json
│   └── hr.json
├── .env.example
└── go.mod
```

---

## go-core features demonstrated

| Feature | Where |
|---|---|
| `logger` — structured slog, file rotation | `cmd/api/main.go` — `logger.Init(...)` |
| `i18n` — JSON translation files, hot-reload | `cmd/api/main.go` — `i18n.Init(...)` |
| `database.Registry` — named connection pools | `cmd/api/main.go` — `reg.MustRegister(...)` |
| `database.Migrate` — GORM AutoMigrate wrapper | `cmd/api/main.go` — `database.Migrate(db, ...)` |
| `database/types` — NullString, NullInt, Float, Bool, NullDateTime | `internal/domain/product/entity.go` |
| `database.TxFromContext` — transaction propagation | `internal/domain/product/repository.go` — `r.db(ctx)` |
| `database.Transactor` — unit-of-work wrapping service calls | `internal/domain/product/service.go` |
| `database.Active` — reusable GORM scope | `internal/domain/product/repository.go` |
| `apperr` — typed errors with HTTP codes and log levels | `internal/domain/product/service.go` |
| `audit.Repository` — structured audit log writes | `internal/domain/product/service.go` |
| `audit.Diff` — struct field diff for change tracking | `internal/domain/product/service.go` — `Update` |
| `auth.Identifiable` + `auth.User[T]` — generic user | `internal/middleware/resolver.go` |
| `auth.Authenticated` — JWT middleware | `cmd/api/main.go` — `registerRoutes` |
| `auth.Authorize` — policy/RBAC middleware | `internal/http/handler/product.go` — `RegisterRoutes` |
| `auth.GeneratePolicy` — namespace:action policies | `internal/http/handler/product.go` |
| `auth.IssueToken` + `auth.ParseToken` | `cmd/api/main.go` — `loginHandler` |
| `auth.MustGetUser[T]` — typed user from gin context | `internal/http/handler/product.go` |
| `auth.Resolver` / `auth.ResolverFunc` | `internal/middleware/resolver.go` |
| `guards.BindRequest[T]` — parse + validate middleware | `internal/http/handler/product.go` — `RegisterRoutes` |
| `guards.MustGetRequest[T]` — typed request from context | `internal/http/handler/product.go` — `Create`, `Update` |
| `datatable` — pagination, search, filter, order | `internal/http/handler/product.go` — `List` |
| `datatable.ParamsFromGin` — parse query params | `internal/http/handler/product.go` — `List` |
| `datatable.NewFilter` | `internal/http/handler/product.go` — `List` |
| `resource.New[T]` — single-record fetcher with meta | `internal/http/handler/product.go` — `Show` |
| `resource.WithAuthorLoader` | `internal/http/handler/product.go` — `Show` |
| `resource.WithCount` | `internal/http/handler/product.go` — `Show` |
| `web.JSON` / `web.Created` / `web.NoContent` | `internal/http/handler/product.go` |
| `web.GetParamInt` | `internal/http/handler/product.go` |
| `web.GinValidationContext` | `cmd/api/main.go` — compile-time check |
| `validation.ValidationContext` | `cmd/api/main.go` — compile-time check |
| `cache.InMemoryCache` | `cmd/api/main.go` |
| `event.InMemoryBus` — publish/subscribe | `cmd/api/main.go` |
| `tenancy.FromAuthenticatedUser` — tenant middleware | `cmd/api/main.go` — `registerRoutes` |
| `router.NewEngine` — pre-configured Gin + all core middleware | `cmd/api/main.go` |
| `bootstrap.NewApp` — lifecycle, graceful shutdown | `cmd/api/main.go` |
| `bootstrap.HealthHandler` + `DBHealthChecker` | `cmd/api/main.go` |
| `database.UseDatabaseConnection` (commented example) | `cmd/api/main.go` — `registerRoutes` |

---

## Running the example

```bash
# 1. Start MySQL
docker run -d \
  --name go-core-db \
  -e MYSQL_ROOT_PASSWORD=secret \
  -e MYSQL_DATABASE=go_core_example \
  -p 3306:3306 \
  mysql:8.0

# 2. Copy and edit the env file
cp .env.example .env

# 3. Run
cd cmd/api && go run main.go
```

The server starts on `:8080`.

---

## API endpoints

| Method | Path | Auth | Policy |
|--------|------|------|--------|
| POST | `/api/v1/auth/login` | No | — |
| GET | `/api/v1/products` | JWT | — |
| GET | `/api/v1/products/:id` | JWT | — |
| POST | `/api/v1/products` | JWT | `products:create` |
| PUT | `/api/v1/products/:id` | JWT | `products:update` |
| DELETE | `/api/v1/products/:id` | JWT | `products:delete` |
| GET | `/health` | No | — |

### Example requests

```bash
# Login (stub — always succeeds)
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"secret"}'

# List products (paginated, searchable)
curl http://localhost:8080/api/v1/products?page=1&per_page=10&search=widget \
  -H "Authorization: Bearer <token>"

# Create a product
curl -X POST http://localhost:8080/api/v1/products \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"Widget Pro","sku":"WGT-001","price":29.99,"stock":100}'

# Get single product with author + audit count in meta
curl http://localhost:8080/api/v1/products/1 \
  -H "Authorization: Bearer <token>"

# Update
curl -X PUT http://localhost:8080/api/v1/products/1 \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"price":24.99}'

# Soft delete
curl -X DELETE http://localhost:8080/api/v1/products/1 \
  -H "Authorization: Bearer <token>"

# Health check
curl http://localhost:8080/health
```

---

## Design notes

### Why Repository + Service + Handler?

Each layer has one job and one dependency direction:

```
Handler → Service → Repository → *gorm.DB
```

- **Handler** knows about HTTP (gin.Context, status codes, request parsing).
- **Service** knows about business rules (uniqueness, audit, transactions).
- **Repository** knows about SQL (GORM, table names, query construction).

Nothing in the service or repository touches `*gin.Context`. This means the
service is fully testable without starting an HTTP server.

### Transaction propagation

`database.Transactor.WithinTransaction` stores the `*gorm.DB` transaction in
`context.Context` via `database.txKey{}`. Every repository's `db(ctx)` method
picks it up automatically:

```go
func (r *gormRepository) db(ctx context.Context) *gorm.DB {
    if tx, ok := database.TxFromContext(ctx); ok {
        return tx.WithContext(ctx)
    }
    return r.conn.WithContext(ctx)
}
```

This means you can call multiple repositories inside one
`transactor.WithinTransaction` and they all share the same DB transaction
without passing `*gorm.DB` explicitly through the call stack.

### Validation flow

1. `guards.BindRequest[T]()` middleware runs first:
   - Parses JSON or multipart body via `binders.BindJSON`
   - Runs stateless rules from struct `validation` tags (`required`, `max`, `email`, `date`)
   - Aborts with 422 if any rule fails
2. The handler calls `guards.MustGetRequest[T](ctx)` to get the validated struct.
3. The service performs stateful checks (DB uniqueness, existence) and returns
   typed `*apperr.AppError` errors that the global error handler serialises.

### Known bugs to fix before production

See the full code review. The three most impactful:

1. **`datatable.Filter`** — change `Query func(*gorm.DB, string, string)` to return
   `*gorm.DB` and assign back in `Get()`. Every filter is currently a no-op.
2. **`datatable`/`resource` `WithQuery`** — callback must return `*gorm.DB`.
3. **`bootstrap` `os.Exit(1)`** — replace with error channel so closers are called.

The `datatable.NewFilter` in `handler/product.go` is already written with the
correct (fixed) signature as a forward reference.
