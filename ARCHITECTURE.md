# go-core architecture

The package dependency rules below are checked by `archtest/dogfood_test.go`
(`go test ./archtest`); a violation fails the build of a pull request. What
they are for: a route declaration is plain data, the domain-facing cores of the
modules know no framework, and only `gocore` knows every layer.

## 1. Package dependency rules

These import relationships must hold after every task. A violation means an
architecture error was introduced.

```
Layer 0 — no internal imports:
  apperr, i18n (types only), navigation, mail (standard library only),
  pagination (paging primitives for a framework-free domain or application)

Layer 1 — imports Layer 0 only:
  validation, logger

Layer 2 — imports Layers 0–1:
  database, database/types (column value types over the driver), auth, event, storage, tenancy, worker

Layer 3 — imports Layers 0–2:
  internal/binders, middlewares, datatable, resource, observability, web, audit, health

Layer 4 — imports Layers 0–3 (and authz):
  bootstrap, frontend, go2ts, route, web/typed (the web helpers that need route)

Layer 4+ — imports route and go2ts, never bootstrap or gocore:
  contract (TypeScript generation from route groups)

Layer 5 — imports Layers 0–4:
  gocore

Layer 6 — test support for Layer 5:
  gocoretest

Test support with no go-core imports (SQLite, MySQL, MariaDB harness):
  database/dbtest

Modules — features go-core ships, each a facade that imports gocore (section 1.2):
  notification (the inbox and e-mail; notification/mailtext is the mail text, notification/migrations the goose files, registered by Install),
  identity (core identity/account imports apperr, authz, mail (Layer 0) and bcrypt only; gormstore,
  identity/http, identity/mailtext and identitytest import the layers below;
  audit/migrations is the audit_logs table's goose file, registered by the
  modules that write an audit trail)
  identity/preferences (a person's preferences object, imports apperr and validation; its gormstore
  imports database, its migrations are the account_preferences goose file; the route is identity/http's)
  lock (named leases; imports database and gocore; its gormstore imports database, its migrations are the
  locks goose file)
  identity/serviceaccount (service accounts and API keys; imports apperr, audit, authz and event;
  its gormstore imports database, its http route and access/accesshttp, and keynotice imports
  notification and gocore, which identity cannot, since notification imports identity/http)
  incident (the incident log: the facade imports gocore, notification, identity/account (the people), route, tenancy,
  audit, authz and its own sub-packages; incident/core is framework-free and takes apperr, pagination and the
  standard library; gormstore imports database, datatable and core; storetest, mailtext and migrations hold the
  conformance suite, the alert wording and the goose files)

Layer 7 — example only:
  go-core-example

Test support for an application's own architecture rules (standard library and
validation only; go-core's section 1 is checked with it, section 1.1):
  archtest
```

Specific prohibitions:
- `auth`     MUST NOT import `database`, `bootstrap`, or `event`
- `event`    MUST NOT import `bootstrap` or `auth`. It imports `database` (the transaction `Publish` joins, the outbox worker) and never `gocore`; `gocore` imports `event` and `event/migrations` for `App.Events`
- `incident/core` MUST NOT import `gocore`, `route`, `gin`, GORM or `database/sql`: it is the service and the ports, which `gormstore`
  implements and the facade wires. `incident` imports `notification` (the alerts); neither imports it. Who holds a permission is
  `authz.Engine.HoldersOf`, which `incident` reaches through the application's authorizer
- `notification` imports `event`, `identity/account` (the people it notifies; `identity/http` for the stream's session re-check), `gocore`, `route`, `database` and `authz`; neither `event` nor `identity` imports it
- `audit`    MUST NOT import `bootstrap` or `auth`
- `database` MUST NOT import `event`, `auth`, or `bootstrap`
- `health`   MUST NOT import `bootstrap`
- `httpclient/internal/resilience` MUST NOT import anything from go-core (pure algorithms)
- `route`   MUST NOT import `bootstrap` or `gocore`: a route declaration is plain
  data, readable (by a generator, for instance) without an application. It
  imports `internal/binders`, `validation`, `web` and `authz` / `authzhttp` for the adapter.
- `gocore`  is the only package that knows both `route` and `bootstrap`; features
  import `gocore` in their `Install` function and nothing in `bootstrap`.
  `bootstrap`, `route`, `database`, `authz` MUST NOT import `gocore` or `gocoretest`.
- `gocoretest` MUST be imported by `_test.go` files only; non-test code never
  imports it.
- A module's framework-free core (`identity/account`) MUST NOT import gin, GORM,
  `database/sql` or any other go-core package but `apperr`, `authz` (types) and
  `mail` (a test holds it). A module's `Install` builds only its own feature's
  services (a test in `gocore` holds it).

Verify with: `go build ./...` — an import cycle produces "import cycle not allowed".

### 1.1 What a test holds

`archtest/dogfood_test.go` runs go-core's own `archtest` over this section, on
production files (`_test.go` files and `*test` support packages are exempt):

- the layers 0 to 6 of the diagram: a package never imports a layer above its own;
- every specific prohibition above, `httpclient/internal/resilience` importing nothing from go-core,
  `notification` imported by neither `event` nor `identity` (the key notice
  excepted), and `gocore` imported only by a module's facade;
- `gocoretest` imported by test code only;
- `incident/core` taking the standard library, `apperr` and `pagination` only;
- `identity/account` taking the standard library, `apperr`, `authz`, `mail` and bcrypt only.

It also holds `archtest`'s route-access rule on go-core's own routes (every route
says who may call it). Not checked: `fmt.Errorf` wrapping (two `%v` wraps of an
error that may be nil remain in `datatable` and `resource`), the route naming
rules (modules build some paths at run time), that an `Install` builds only
its own services (`gocore` has its own test) and the import cycle, which the compiler refuses.

### 1.2 Module layout

A module is a feature go-core ships (identity, access, notifications, ...). It
is the same shape as an application's own feature, so the package layout is a
convention, not a framework. `authz` is the existing example.

```
go-core/<module>/            the facade: Install, options, the public interfaces apps use.
                             The only package that imports gocore.
  <core>/                    framework-free: types, ports, services (apperr, validation only)
  gormstore/                 the default SQL adapter (GORM), MariaDB 10.3 safe
  storetest/                 the conformance suite every store passes
  http/                      typed routes (route.Get ...), inputs, DTOs, and
                             Routes = route.Group("<module>", ...)
  <module>test/              memory stores, fakes, a fixed clock
  migrations/                //go:embed *.sql; goose files <release-date>_<name>.sql
```

The template of the facade:

```go
// Install puts the module into app and returns what other features need.
func Install(app *gocore.App, opts ...Option) Users {
    cfg := options(opts)
    store := gormstore.New(app.Database(cfg.conn))

    app.Migrations(migrations.Files, cfg.conn...) // the connection the store lives on
    app.Permissions(Permissions)
    app.Routes(http.Show.To(handler.Show))

    return newUsers(store, app.Clock())
}
```

Rules for the migrations of a module:

- Flat `embed.FS` of goose files; `Install` hands it over with its connection
  (`app.Migrations(files)` for the primary, `app.Migrations(files, Shared)` for
  another). The application's own migrations stay one directory per connection
  (`app.MigrationsByConnection`).
- The version is the release date (`20261015000000_authz_roles.sql`); several
  files of one release count up in the last digits. Two files with one version on
  one connection stop start-up and are both named.
- A file is immutable once a tag contains it, `-rc` tags included: an
  application may already have applied it. A change is a new file.
- Files follow the application rules: first line `-- +goose Up`, `--` comments,
  one change per file, `IF NOT EXISTS` where MariaDB allows. They run on
  MariaDB 10.3 and MySQL; `dbtest.RequirePortable(t, migrations.Files)` is the
  test every module has.
- A schema test holds the files equal to the GORM models the store uses (see
  `authz/gormstore/schema_test.go`).

Release rule: a module is tagged `vX.Y.Z-rc.N` until the new application has
used it; only then does the plain tag follow. Until then its API may still
change between rc tags.

---

## 2. Package map

| Package | What it is |
|---|---|
| `gocore` | The application: `gocore.New`, `Install`-style features, typed routes, `Run`, commands, checks. The only package a feature imports to wire itself. |
| `gocoretest` | A `gocore.App` for tests: in-memory SQLite that migrates as features install, a fixed clock, routes called without a port. |
| `bootstrap` | Lifecycle, configuration and the HTTP server the App runs on (`Config`, `AppBuilder`, `Component`). |
| `route` | Typed route declarations (`Get`, `Post`, `Raw`, `Group`, `.Requires`, `.Public`), plain data. |
| `contract` | TypeScript (types, Zod, route tables) generated from route groups; `go2ts` is its type reflection. |
| `web`, `web/typed`, `web/upload`, `web/webtest` | Handler helpers, body and path limits, typed-handler helpers, multipart uploads, response-shape assertions. |
| `middlewares` | gin middleware: request id and log, error mapping, security headers, CORS, rate limits, idempotency, recovery. |
| `frontend` | The SPA shell: Vite assets, CSP nonce, the state builder. |
| `authz`, `authz/*` | Permissions, roles, bindings and the authorizer; its gin adapter, GORM scope, store, test helpers and TypeScript union. |
| `auth` | Authentication primitives: the authenticating middleware, the user in the request context, API keys, the password hasher, token storage; `auth/server` the authentication server. |
| `identity`, `identity/*` | People: accounts, sign-in, sessions, preferences, service accounts and API keys. |
| `access`, `access/*` | Role and binding administration (routes and service) over `authz`. |
| `notification`, `notification/*` | The in-app inbox, e-mail and Web Push of notifications. |
| `incident`, `incident/*` | The incident log: server errors become deduplicated incidents with alerts. |
| `savedview`, `savedview/*` | A person's named list filters. |
| `lock`, `lock/*` | Named leases across processes. |
| `retention` | Deleting or redacting old rows by policy. |
| `event` | The transactional outbox: `Define`, `Publish`, `Queue`. |
| `worker` | Background workers and their manager. |
| `database`, `database/*` | GORM connection registry, transactions, goose migrations (`migrate`), value types, test harness (`dbtest`), SQLC scaffold. |
| `datatable`, `resource`, `pagination` | List queries with filters and paging; generic resource access; framework-free paging primitives. |
| `storage`, `storage/*` | Files on named disks (local, memory, pool, credentials). |
| `mail`, `pdf`, `httpclient`, `i18n`, `navigation` | E-mail, PDF building, an outbound HTTP client with retry and circuit breaker, translations, the menu tree. |
| `validation`, `apperr`, `tenancy`, `audit`, `logger`, `health`, `ratelimit` | Struct-tag validation, the structured error type, tenant context, the audit trail, logging, health probes, rate limiting. |
| `observability`, `observability/*` | Metrics, tracing and alerts. |
| `archtest` | An application's architecture rules, checked from source; go-core runs it on itself. |
| `internal/*` | Binders (request binding), reflection cache, the end-to-end harness. |
| `cmd/go-core`, `cmd/devserver`, `cmd/modulets` | The scaffold CLI (`new feature`), a development server, and the module TypeScript generator. |
| `go-core-example` | A runnable sample application with its own `go.mod`. |
