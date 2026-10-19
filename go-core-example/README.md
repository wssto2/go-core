# go-core Example Application

A small but complete product catalog application demonstrating the main `go-core`
patterns in a realistic SSR-friendly shape. Uses Gin + GORM + MySQL, structured
around a single **Product** domain.

The frontend under `frontend/` is a **Vue 3 + Vite catalog UI** rendered through
the Go backend to demonstrate an SSR-first, modular frontend convention:

- backend API routes live under `/api/...`
- non-API routes render the SPA shell
- request-scoped state is injected into `window.APP_STATE`
- translated SEO metadata comes from `i18n/*.json`
- in development, assets are served from the Vite dev server
- in production, assets are resolved from the Vite manifest

---

## Directory layout

```
go-core-example/
├── cmd/
│   └── api/
│       ├── main.go            # Entrypoint — gocore.New, then one Install per feature, OTel, go2ts
│       ├── app_state.go       # Page shell composer (SEO head + window.APP_STATE)
│       └── page_data_module.go # GET /__page-data for client-side navigations
├── frontend/
│   ├── src/
│   │   ├── main.ts      # Vue entrypoint + app-state plugin + router install
│   │   ├── App.vue      # Root app hosting RouterView
│   │   ├── router/      # Vue Router page map
│   │   ├── state/       # Shared injected app-state reader
│   │   ├── types/       # Single source of truth for frontend contracts
│   │   ├── layouts/     # Page shell/layout components
│   │   ├── pages/       # Route-level page components
│   │   ├── components/  # Reusable catalog/detail UI sections
│   │   ├── composables/ # Derived page snapshot helpers
│   │   ├── utils/       # Formatting helpers
│   │   └── styles/      # Shared frontend styles
│   ├── templates/
│   │   └── index.html   # SPA shell rendered by go-core
│   ├── dist/
│   │   └── .vite/
│   │       └── manifest.json # Production asset manifest
│   ├── package.json
│   └── vite.config.ts
├── i18n/
│   ├── en.json          # Example SEO translations used by the page shell
│   └── hr.json          # Croatian metadata translations for the same routes
├── internal/
│   └── domain/
│       ├── access/
│       │   ├── catalogue.go   # Permissions, hierarchy and predefined roles of the authz demo
│       │   ├── places.go      # Dealers/locations as authz scopes (resolver + names)
│       │   ├── routes.go      # Typed lead routes (.Requires) and their contract
│       │   ├── service.go     # Leads filtered by what the caller may see
│       │   └── install.go     # Install(app, engine): routes, demo data on start-up
│       └── product/
│           ├── entity.go              # GORM model + ImageStatus constants
│           ├── options.go             # CreateProductOptions, UpdateProductOptions (validated)
│           ├── requests.go            # Typed route inputs
│           ├── events.go              # event.Define declarations (queued in the outbox)
│           ├── repository.go          # Repository interface + GORM implementation
│           ├── service.go             # Business logic (audit, transactions, events)
│           ├── service_instrumented.go # Observability wrapper (metrics + tracing per method)
│           ├── routes.go              # Typed routes: permissions, handlers, the upload route
│           ├── image_worker.go        # Event consumer: thumbnail + medium variants
│           └── install.go             # Install(app, storageDir): service, routes, consumer
├── Makefile
└── go.mod
```

---

## go-core features demonstrated

| Feature | Where |
|---|---|
| `gocore.New`, `gocore.WithPrefix`, `gocore.WithDatabase`, `gocore.WithSPA`, `gocore.WithRateLimit`, `gocore.WithIdempotency` | `cmd/api/main.go` |
| `Install(app)` per feature, `main` reads top to bottom | `cmd/api/main.go` |
| `app.Env()` — explicit, validated settings; `gocore.New` reads `APP_*`, `LOCAL_DB_*`… itself | `cmd/api/main.go` |
| `identity.Install` — sign-in, sessions, the app's authentication | `cmd/api/main.go` |
| `access.Install` — roles, bindings, the app's authorizer | `cmd/api/main.go` |
| `route.Get` / `Post` / `Raw` — routes declared as values, `.Requires` | `internal/domain/product/routes.go` |
| `route.Page`, `route.WithMeta` — list and show envelopes | `internal/domain/product/routes.go` |
| `authz` — catalogue, hierarchy, roles, `authzgorm.Filter` | `internal/domain/access/` |
| `database.Registry` — named connection pools | `cmd/api/main.go` (`app.Database()`) |
| `database.Transactor` — unit-of-work wrapping service calls | `internal/domain/product/service.go` |
| `database.TxFromContext` — transaction propagation | `internal/domain/product/repository.go` |
| `database/types` — NullString, NullInt, Float, Bool | `internal/domain/product/entity.go` |
| `apperr` — typed errors with HTTP codes and log levels | `internal/domain/product/service.go` |
| `audit.Repository` — structured audit log writes | `internal/domain/product/service.go` |
| `audit.Diff` — struct field diff for change tracking | `internal/domain/product/service.go` — `Update` |
| `event.Define` + `app.Events` — transactional outbox, consumers | `internal/domain/product/events.go`, `install.go` |
| `ratelimit.NewInMemoryLimiter` | `cmd/api/main.go` |
| `datatable` — pagination, search, filter, order | `internal/domain/product/routes.go` — `list` |
| `datatable.ParamsFromValues` — parse query params | `internal/domain/product/routes.go` |
| `resource` — single-record fetcher with meta | `internal/domain/product/routes.go` — `show` |
| `web/upload.ValidateFile` — multipart file upload | `internal/domain/product/routes.go` — `uploadImage` |
| `go2ts.GenerateTypes` — TypeScript type generation | `cmd/api/main.go` (at startup) |
| `observability.ServiceObserver` — metrics wrapper | `internal/domain/product/install.go` |
| `observability/tracing` — OpenTelemetry init | `cmd/api/main.go` |

---

## Running the example

### 1. Start MySQL

```bash
docker run -d \
  --name go-core-db \
  -e MYSQL_ROOT_PASSWORD=secret \
  -e MYSQL_DATABASE=go_core_example \
  -p 3306:3306 \
  mysql:8.0
```

### 2. Install frontend dependencies

From the `go-core-example/frontend` directory:

```bash
npm install
```

### 3. Run the backend

From the `go-core-example` directory:

```bash
make run
```

Or directly:

```bash
go run ./cmd/api/
```

> **Note:** `go run cmd/api/main.go` will not work because `app_state.go` is a
> separate file in the same package. Always use `go run ./cmd/api/` or `make run`.

The backend starts on `:8080`.

### 4. Run the SPA in development

From the `go-core-example/frontend` directory:

```bash
npm run dev
```

Vite starts on `http://localhost:5173` and the Go server will automatically use
the dev server assets for non-API routes.

### 5. Production-style frontend build

From the `go-core-example/frontend` directory:

```bash
npm run build
```

This writes the production bundle and Vite manifest into `frontend/dist/`. The
Go server resolves built assets from that manifest when the Vite dev server is
not running.

---

## SPA frontend behavior

The example backend uses the builder-based SPA integration:

- `gocore.WithSPA(...)` registers the SPA shell
- `/api/...` routes continue to return JSON
- all other routes fall back to `frontend/templates/index.html`
- `cmd/api/app_state.go` composes a full page shell model
- only the bootstrap portion is injected into `window.APP_STATE`
- the Vue app reads that payload once and injects it through a shared app-state module
- Vue Router owns page-level rendering for `/`, `/products`, and `/products/:id`
- `GET /__page-data?path=/...` returns route-scoped page shell JSON for client-side navigations

The example now splits page data into two parts:

1. `pageHeadData`
   - `Title`
   - `MetaDescription`
   - `MetaKeywords`

2. `pageBootstrapState`
   - `appName`
   - `env`
   - `locale`
   - `path`
   - `apiBase`
   - server-composed `catalog` data
   - optional `product` details for `/products/:id`
   - optional `viewer`
   - optional `viewerError`

This is a good default pattern for applications using Vue 3 + Vite with
frontend files stored under `frontend/`.

On the frontend side, the example is now split into:

- one shared `AppState` contract in `frontend/src/types/app-state.ts`
- one reactive app-shell store in `frontend/src/state/app-state.ts`
- page-level components under `frontend/src/pages/`
- reusable sections under `frontend/src/components/`
- a small router in `frontend/src/router/index.ts`

That keeps route rendering, state reading, layout, and formatting concerns
separate instead of growing a single root component.

The important detail is that router navigation no longer depends on shipping the
entire catalog everywhere. Product detail pages use route-scoped bootstrap data,
and Vue Router asks the backend for the next page shell through `__page-data`
before rendering the next route on the client.

The responsibility split is:

- `catalogPageShellComposer.ComposePageShell(...)`
  - decides translated head metadata for the requested page
  - builds bootstrap state for Vue, including the catalog grid
  - composes product detail state for `/products/:id`
  - calls backend services/resolvers as needed
- `spaShellDataBuilder.Build(...)`
  - adapts `WithSPA(...)`'s `func(*gin.Context) (any, error)`
  - converts the Gin request into a small request DTO
  - delegates all real decisions to the composer

This keeps dependencies visible: the composer takes the catalogue queries as
plain functions. It translates `<title>`, `<meta name="description">`,
and `<meta name="keywords">` from the example's i18n files, bootstraps a
product catalog by calling `productMod.ListCatalog(...)`, and composes a single
product view by calling `GetCatalogProduct(...)`. The signed-in person is not in
the shell: the client reads it from `GET /api/v1/auth/me`.

The example resolves locale from `?lang=<code>` first, then from the
`Accept-Language` header, and finally falls back to `cfg.I18n.DefaultLocale`.

If your app has multiple server-rendered pages, keep the same split:

- one composer per page (`catalogPageShellComposer`, `settingsPageShellComposer`, ...)
- one small router/dispatcher implementing the same `pageShellComposer` interface
- one `spaShellDataBuilder` adapting that dispatcher to `WithSPA(...)`

## API endpoints

| Method | Path | Auth | Notes |
|--------|------|------|-------|
| POST | `/api/v1/auth/login` | No | identity sign-in; sets the session cookies |
| GET | `/api/v1/auth/me` | Session | The session payload, with the person's access |
| GET | `/api/v1/products` | Session | Paginated datatable |
| GET | `/api/v1/products/:id` | Session | Single record with meta |
| POST | `/api/v1/products` | `catalog.product:create` | Creates a product |
| PUT | `/api/v1/products/:id` | `catalog.product:update` | Updates a product |
| DELETE | `/api/v1/products/:id` | `catalog.product:delete` | Soft-deletes a product |
| POST | `/api/v1/products/:id/image` | `catalog.product:update` | Uploads a product image |
| GET | `/api/v1/leads`, `/api/v1/leads/:id` | `sales.lead:view` | Leads the caller may see |
| PUT | `/api/v1/leads/:id` | `sales.lead:update` | Renames a lead the caller may update |
| GET | `/metrics` | No | Prometheus metrics |
| GET | `/health` | No | Liveness probe |
| GET | `/ready` | No | Readiness probe |

### Example requests

```bash
# Run the migrations once (identity and access own their tables), then serve:
#   go run ./cmd/api migrate && go run ./cmd/api
# Login as the demo webmaster (password "demo"); the session cookie is kept in the jar
curl -c jar.txt -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"login":"admin","password":"demo"}'

# List products (paginated, searchable)
curl "http://localhost:8080/api/v1/products?page=1&per_page=10&search=widget" \
  -b jar.txt

# Create a product
curl -X POST http://localhost:8080/api/v1/products \
  -b jar.txt \
  -H "Content-Type: application/json" \
  -d '{"name":"Widget Pro","sku":"WGT-001","price":29.99,"stock":100}'

# Get single product with meta
curl "http://localhost:8080/api/v1/products/1" \
  -b jar.txt

# Update
curl -X PUT http://localhost:8080/api/v1/products/1 \
  -b jar.txt \
  -H "Content-Type: application/json" \
  -d '{"price":24.99}'

# Upload product image (multipart)
curl -X POST http://localhost:8080/api/v1/products/1/image \
  -b jar.txt \
  -F "image=@/path/to/photo.jpg"

# Soft delete
curl -X DELETE http://localhost:8080/api/v1/products/1 \
  -b jar.txt

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
