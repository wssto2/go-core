# go-core

## Overview

go-core is a modular Go backend framework that provides a shared foundation for building production-grade services.

It standardizes:

* Authentication & authorization
* Database access & transactions
* Validation & binding
* Error handling
* Middleware & HTTP layer
* Audit logging
* Multi-tenancy
* Event system
* Worker processes

The goal is to eliminate boilerplate and enforce **safe, predictable patterns** across all services.

---

## Key Principles

* Strong typing over dynamic behavior
* Explicit architecture (no hidden magic)
* Thin handlers, fat services
* Centralized error handling via `apperr`
* Context-driven request lifecycle
* Modular design (plug-and-play packages)

---

## Project Structure

### Core Packages

* `gocore` → assembles an application from features: `New`, `Install`-style features, `Later`, `Run` (see "Installing features")
* `route` → typed routes: `route.Get[In, Out](path).Requires(permission)`, binding, validation, permission check
* `gocoretest` → a `gocore.App` for tests: in-memory SQLite, fixed clock, `Do` / `Decode` to call routes
* `bootstrap` → application lifecycle, DI container, config (what `gocore` is built on)
* `web` → HTTP handling, response format, helpers
* `auth` → authentication, JWT, policies, middleware
* `authz` → roles, bindings at a scope, per-record access, delegation, list filtering (see `authz/doc.go`; sub-packages `authzgorm`, `gormstore`, `authzhttp`, `authzts`, `authztest`, `storetest`)
* `database` → repositories, transactions, types
* `validation` → input validation system
* `binders` → request binding (JSON, multipart)
* `middlewares` → HTTP middleware (auth, logging, security)
* `worker` → background workers

### Feature Packages

* `audit` → audit logging and diff tracking
* `datatable` → filtering, pagination, query helpers
* `tenancy` → multi-tenant context + DB scoping
* `event` → event bus abstraction

### Utility Packages

* `apperr` → structured application errors
* `i18n` → localization helpers
* `go2ts` → Go → TypeScript type generation
* `utils` → generic helpers

### Example App

* `go-core-example` → reference implementation

---

## How It Works

### Request Flow

1. HTTP request enters via `engine/gin.go`
2. Middleware chain executes (`middlewares`)
3. Request is bound using `binders`
4. Input validated via `validation`
5. Handler calls service layer
6. Service uses repositories (`database`)
7. Errors returned as `apperr.AppError`
8. Response formatted via `web/response`

---

## Installing features

A feature is a package with one `Install` function. Collaborators are ordinary arguments; app-wide values (database, clock, config, logger) come from the `*gocore.App`. The snippets below are taken from the compiled `Example` functions in `route`, `gocore` and `gocoretest`.

**Declare routes as values** (`route/example_test.go`). The declaration can be read without running anything; the handler is a plain function the compiler ties to it. Input fields say where they come from: `path:"id"`, `query:"page"`, or `json:"title"` for the body.

```go
var (
    Show   = route.Get[ShowInput, Ticket]("/tickets/:id").Name("tickets.show").Requires(PermView)
    Close  = route.Delete[ShowInput, route.Empty]("/tickets/:id") // 204
    Routes = route.Group("tickets", Show, Close)
)

func showTicket(ctx context.Context, in ShowInput) (Ticket, error) { ... }
```

What the typed form cannot express (Server-Sent Events, downloads, uploads) is declared with `route.Raw(method, path)` (same `.Name`, `.Requires`) and bound the same way with `.To(ginHandler)`; it is part of the route table without input or output types.

**Install the feature** (`gocore/example_test.go`):

```go
func Install(app *gocore.App) {
    service := &Service{Clock: app.Clock()}

    _ = app.Database()          // the primary connection; app.Database(Shared) another one

    app.Routes(Ping.To(service.Ping))
}
```

**Secure by default.** Every route needs an authenticated principal unless it is declared `.Public()`; `.Requires(perm)` adds a permission check on top. The application sets authentication once, with the middleware go-core already has, and non-public routes are mounted behind it:

```go
app := gocore.New(cfg,
    gocore.WithAuthentication(auth.Authenticated(provider), authzhttp.Principals(resolve)),
    gocore.WithAuthorizer(engine))

var Login = route.Get[route.None, Form]("/login").Public()   // anyone, signed in or not
```

An unauthenticated request to a non-public route gets the usual 401 envelope. A non-public route with no authentication configured is a start-up problem ("pass gocore.WithAuthentication(...) to gocore.New, or mark the route .Public()"). `route.Spec.Public` is part of the declaration, so generators can see it.

`const Shared database.Connection = "shared"` names a connection once; `Registry.Database(Shared)` and `app.Database(Shared)` take it, and `Registry.Get("shared")` keeps working.

**`main` reads top to bottom.** A feature used before the line that creates it does not compile (`gocore/testdata`, built by `TestInstallOrderIsCheckedByTheCompiler`):

```go
func main() {
    app := gocore.New(config, gocore.WithAuthorizer(engine))
    app.Permissions(permissions.All)

    users := identity.Install(app)
    tickets.Install(app, users)

    if err := app.Run(); err != nil { log.Fatal(err) }
}
```

`Install` only collects (`Routes`, `Background`, `Permissions`, `Modules`); nothing runs until `Run`, so the order of those never matters. Old-style `bootstrap.Module`s run in the same app through `app.Modules(...)`, so an application moves to `Install` one feature at a time.

**A real cycle between two features uses `gocore.Later`:**

```go
reads := gocore.Later[contracts.VehicleReads](app)
vehicles := vehicles.Install(app, reads)
crm := crm.Install(app, vehicles)
reads.Set(crm.VehicleReads) // inside vehicles: reads.Get()
```

**`Run` checks first and says what to fix.** Every problem is collected, each with its fix (`ExampleApp_Check`):

```
gocore: cannot start, 2 problem(s):
  1. database: connection "shared" not found ... (registered connections: local). Fix: register the connection in the database config, or fix its name
  2. gocore.Later[gocore_test.Reads] was never set. Fix: call Set on it once every feature that needs it is installed
```

The check covers an unset `Later`, a declared route with no handler (typed or raw), a `Requires` permission the catalogue does not define, a non-public route with no authentication or a permission route with no authorizer, duplicate or refused routes, and an unknown connection. Then it boots; a failed boot stops what already started, in reverse. Shutdown order: HTTP, background workers and modules, database last.

**Test a feature with plain calls** (`gocoretest/example_test.go`):

```go
app := gocoretest.New(t, gocoretest.SignedIn(authz.User(7, 0))) // in-memory SQLite, fixed clock, test logger; requests are signed in as user 7 (without SignedIn they are anonymous: 401 except Public routes)
Install(app)

rec := gocoretest.Do(t, app, http.MethodGet, "/tickets/7", nil)
ticket := gocoretest.Decode[Ticket](t, rec)
```

---

## Error Handling

All errors use `apperr.AppError`:

* Structured error codes
* Log level hints
* Field-level validation errors

Example:

```go
return apperr.BadRequest("invalid input")
```

---

## Authentication

* JWT-based authentication
* Policy-based authorization (`namespace:action`)
* Context-based user injection

```go
user := auth.MustGetUser[MyUser](ctx)
```

---

## Database

* GORM-based repositories
* Transaction support via context
* Custom nullable & typed fields
* SQL migrations per connection (`database/migrate`, goose): `migrations/<connection>/<yyyymmddhhmmss>_<name>.sql`, each database records its own versions; `MarkApplied` adopts a database whose changes ran by hand. `go-core new migration <name> -c <connection>` creates one

---

## Audit Logging

* Tracks entity changes
* Stores before/after state
* Uses reflection-based diff

---

## Multi-Tenancy

* Context-based tenant resolution
* Optional DB query scoping

---

## Rules

* Do not bypass `apperr`
* Do not access DB outside repositories
* Do not put business logic in handlers
* Always pass `context.Context`
* Prefer interfaces over concrete types

---

## Purpose

This repository is a **core library**, not an application.

All business logic should live in consuming services.

---
