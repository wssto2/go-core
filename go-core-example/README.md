# go-core Example Application

A minimal but complete Go application demonstrating every feature of `go-core`.
Uses Gin + GORM + MySQL, structured around a single **Product** domain.

---

## Directory layout

```
go-core-example/
├── cmd/
│   └── api/
│       ├── main.go      # Entrypoint — wires bootstrap, OTel, go2ts
│       └── config.go    # Config loading via bootstrap.EnvStr/EnvInt
├── internal/
│   └── domain/
│       ├── auth/
│       │   ├── entity.go    # User model + policy helpers
│       │   ├── resolver.go  # Note: IdentityResolver is now Module.IdentityResolver (DB-backed)
│       │   ├── roles.go     # AccountType / AccountRole entity patterns (reference)
│       │   └── module.go    # Auth module — login endpoint + DB resolver + user migration
│       └── product/
│           ├── entity.go              # GORM model + ImageStatus constants
│           ├── options.go             # CreateProductOptions, UpdateProductOptions (validated)
│           ├── requests.go            # HTTP request/response structs
│           ├── events.go              # Domain events with JSON tags
│           ├── repository.go          # Repository interface + GORM implementation
│           ├── service.go             # Business logic (resilience, audit, transactions, outbox)
│           ├── service_instrumented.go # Observability wrapper (metrics + tracing per method)
│           ├── handler.go             # Gin handlers (datatable, upload, auth guards, 10MB limit)
│           ├── image_worker.go        # Background image processing (thumbnail + medium)
│           ├── webhook.go             # OutboxWorker PublishFunc (webhook + crash-recovery routing)
│           ├── worker.go              # Package stub (productWorker removed — use outbox instead)
│           └── module.go              # Product module — routes + worker lifecycle
├── templates/           # HTML templates (if any)
├── Makefile
└── go.mod
```

---

## go-core features demonstrated

| Feature | Where |
|---|---|
| `bootstrap` — `DefaultInfrastructure`, graceful shutdown | `cmd/api/main.go` |
| `bootstrap.EnvStr` / `EnvInt` — env-based config | `cmd/api/config.go` |
| `bootstrap.Module` — Register / Boot / Shutdown lifecycle | `internal/domain/*/module.go` |
| `bootstrap.MustResolve[T]` — IoC container | `internal/domain/product/module.go` |
| `database.Registry` — named connection pools | `cmd/api/main.go` (via `DefaultInfrastructure`) |
| `database.Transactor` — unit-of-work wrapping service calls | `internal/domain/product/service.go` |
| `database.TxFromContext` — transaction propagation | `internal/domain/product/repository.go` |
| `database/types` — NullString, NullInt, Float, Bool | `internal/domain/product/entity.go` |
| `apperr` — typed errors with HTTP codes and log levels | `internal/domain/product/service.go` |
| `audit.Repository` — structured audit log writes | `internal/domain/product/service.go` |
| `audit.Diff` — struct field diff for change tracking | `internal/domain/product/service.go` — `Update` |
| `auth.IssueToken` + `auth.Claims` | `internal/domain/auth/module.go` — `login` |
| `auth.Authenticated` — JWT middleware | wired via `bootstrap.WithJWTAuth` |
| `auth.Authorize` / `auth.GeneratePolicy` — RBAC | `internal/domain/product/handler.go` |
| `auth.MustGetUser[T]` — typed user from gin context | `internal/domain/product/handler.go` |
| `auth.IdentityResolver` | `internal/domain/auth/resolver.go` |
| `middlewares.BindRequest[T]` — parse + validate | `internal/domain/product/handler.go` |
| `middlewares.MustGetRequest[T]` — typed request | `internal/domain/product/handler.go` |
| `middlewares.RateLimit` — per-user, per-endpoint | `internal/domain/product/module.go` |
| `ratelimit.NewInMemoryLimiter` | `internal/domain/product/module.go` |
| `resilience.Retry` — transient DB failure retry | `internal/domain/product/service.go` — `GetByID` |
| `resilience.CircuitBreaker` — SKU uniqueness guard | `internal/domain/product/service.go` — `Create` |
| `datatable` — pagination, search, filter, order | `internal/domain/product/handler.go` — `list` |
| `datatable.ParamsFromGin` — parse query params | `internal/domain/product/handler.go` |
| `resource` — single-record fetcher with meta | `internal/domain/product/handler.go` — `show` |
| `web.JSON` / `web.Handle` / `web.NoContent` | `internal/domain/product/handler.go` |
| `web/upload.UploadFile` — multipart file upload | `internal/domain/product/handler.go` — `uploadImage` |
| `go2ts.GenerateTypes` — TypeScript type generation | `cmd/api/main.go` (at startup) |
| `event.Bus` — publish/subscribe | `internal/domain/product/service.go` + `worker.go` |
| `worker.Manager` — background worker lifecycle | `internal/domain/product/module.go` |
| `tenancy.FromAuthenticatedUser` — tenant middleware | `internal/domain/product/module.go` |
| `observability.ServiceObserver` — metrics wrapper | `internal/domain/product/module.go` |
| `observability/tracing` — OpenTelemetry init | `cmd/api/main.go` |

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

# 2. Run (from the go-core-example directory)
make run

# Or directly:
go run ./cmd/api/
```

> **Note:** `go run cmd/api/main.go` will not work because `config.go` is a
> separate file in the same package. Always use `go run ./cmd/api/` or `make run`.

The server starts on `:8080`.

---

## API endpoints

| Method | Path | Auth | Notes |
|--------|------|------|-------|
| POST | `/api/v1/auth/login` | No | Returns a signed JWT |
| GET | `/api/v1/products` | JWT | Paginated datatable |
| GET | `/api/v1/products/:id` | JWT | Single record with meta |
| POST | `/api/v1/products` | JWT + `products:create` | Creates a product |
| PUT | `/api/v1/products/:id` | JWT + `products:update` | Updates a product |
| DELETE | `/api/v1/products/:id` | JWT + `products:delete` | Soft-deletes a product |
| POST | `/api/v1/products/:id/image` | JWT + `products:update` | Uploads a product image |
| GET | `/metrics` | No | Prometheus metrics |
| GET | `/health` | No | Liveness probe |
| GET | `/ready` | No | Readiness probe |

### Example requests

```bash
# Login — returns { "data": { "token": "<jwt>" } }
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"secret"}'

TOKEN="<paste token here>"

# List products (paginated, searchable)
curl "http://localhost:8080/api/v1/products?page=1&per_page=10&search=widget" \
  -H "Authorization: Bearer $TOKEN"

# Create a product
curl -X POST http://localhost:8080/api/v1/products \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Widget Pro","sku":"WGT-001","price":29.99,"stock":100}'

# Get single product with meta
curl "http://localhost:8080/api/v1/products/1" \
  -H "Authorization: Bearer $TOKEN"

# Update
curl -X PUT http://localhost:8080/api/v1/products/1 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"price":24.99}'

# Upload product image (multipart)
curl -X POST http://localhost:8080/api/v1/products/1/image \
  -H "Authorization: Bearer $TOKEN" \
  -F "image=@/path/to/photo.jpg"

# Soft delete
curl -X DELETE http://localhost:8080/api/v1/products/1 \
  -H "Authorization: Bearer $TOKEN"

# Health / readiness
curl http://localhost:8080/health
curl http://localhost:8080/ready
```

---

## Design notes

### Why Repository + Service + Handler?

Each layer has one job and one dependency direction:

```
Handler → Service → Repository → *gorm.DB
```

- **Handler** knows about HTTP (gin.Context, status codes, request parsing).
- **Service** knows about business rules (uniqueness, audit, transactions, resilience).
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

1. `middlewares.BindRequest[T]()` middleware runs first:
   - Parses JSON or multipart body
   - Runs stateless rules from struct `validation` tags (`required`, `max`, `email`, `date`)
   - Aborts with 422 if any rule fails
2. The handler calls `middlewares.MustGetRequest[T](ctx)` to get the validated struct.
3. The service performs stateful checks (DB uniqueness, existence) and returns
   typed `*apperr.AppError` errors that the global error handler serialises.

