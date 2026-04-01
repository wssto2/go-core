# GO-CORE ARCHITECTURE — v2 Refactoring

## 1. Purpose of This Document

This document describes the architectural principles, package-level impact map,
and patterns that govern the v2 refactoring work defined in PLAN.md. Every task
in PLAN.md must conform to the rules here. When a task description and this
document conflict, this document wins. Re-read this document at the start of
every new phase.

---

## 2. What This Refactoring Covers

The review identified 48 distinct issues across ten categories:

| Category              | Risk   | Examples                                                     |
|-----------------------|--------|--------------------------------------------------------------|
| Bug Fixes             | 🔴 High | Write-after-Shutdown panic, double registration, bad errors  |
| Dead Code             | 🟡 Med  | Unparsed binder rules, internal/config, worker example file  |
| Duplicate Logic       | 🟡 Med  | Two idempotency stores, two test-helper files, two DI impls  |
| Performance           | 🟠 Med  | Sequential count queries, unquoted ORDER BY, string isPanic  |
| DI Container          | 🟠 Med  | Replace bootstrap.Container with cycle-detecting version     |
| Naming / DX           | 🟡 Med  | BindJSON handles multipart, NewTestDB returns Registry       |
| Logic Relocation      | 🟡 Med  | Health pkg, upload pkg, ResolveTransactor in wrong place     |
| Feature Additions     | 🟢 Low  | Logger in NATS, List/Exists on Driver, audit error callback  |
| Auth Globals          | 🔴 High | Mutable DefaultAuthorizer, package-level HashPassword        |
| Error Typing          | 🟠 Med  | fmt.Errorf used where apperr.* is required                   |

---

## 3. Non-Negotiable Engineering Principles

### 3.1 Read Before You Write

Before modifying any file, read the ENTIRE file using the read_file tool.
Do not rely on summaries shown earlier in the conversation. Import paths, struct
field names, function signatures, and constant names must be verified from the
live file, not from memory.

### 3.2 One Task at a Time

Each PLAN.md task is atomic. Do not combine tasks. Do not start task N+1 before
task N is verified by a passing test and explicitly approved by the user.

### 3.3 No Logic Deletions Without Verified Zero References

Before deleting any exported symbol (function, type, var, const, interface method),
run: grep -r "SymbolName" go-core/ --include="*.go"
Only delete when that command returns zero lines outside the owning file.

### 3.4 No Deprecated Aliases in v2

The framework is not published. There are no external callers. Renamed symbols
must be updated at every call site in the same task. Do NOT add deprecated
one-liner wrappers pointing to the new name. Delete the old name entirely and
update all callers.

### 3.5 No App Logic in Framework Core

go-core packages must never contain hardcoded business concepts (User, Order,
AccountRole). The framework provides interfaces; applications provide
implementations. The go-core-example/ directory is the only place for app logic.

### 3.6 Zero Warnings

After every task, both of these commands must exit with code 0 and print nothing:
  go build ./...
  go vet ./...

### 3.7 Every Code Change Requires a Test

Every task that modifies production code must add or update a _test.go file.
The test must assert the specific behavior the task fixes or adds.
Tests live in the same directory as the code, same package name.

### 3.8 Exact Module Path

The Go module path is: github.com/wssto2/go-core
Every internal import starts with this prefix. Verify in go.mod before writing
any import block. Never shorten or guess the path.

### 3.9 Use apperr for Cross-Package Errors

Any error that crosses a package boundary or is handled by the HTTP ErrorHandler
middleware must be an *apperr.AppError. Use:
  apperr.BadRequest(message)      — invalid client input
  apperr.NotFound(message)        — missing resource
  apperr.Internal(err)            — unexpected system failure
  apperr.Wrap(err, message, code) — wrapping with new context
  apperr.WrapPreserve(err, msg)   — wrapping, preserving original code

fmt.Errorf is only acceptable for internal sentinel errors consumed within the
same package and never reaching the HTTP layer.

### 3.10 Never Silently Drop Errors

The pattern _ = someFunc() is forbidden unless:
  a) The function is a deferred cleanup (e.g., defer resp.Body.Close()), OR
  b) A comment immediately after the line explains exactly why the error is safe
     to ignore in this specific context.

---

## 4. Package Dependency Rules (INVARIANT)

These import relationships must hold true after every task. A violation means you
have made an architecture error.

```
Layer 0 — no internal imports:
  apperr, utils, i18n (types only)

Layer 1 — imports Layer 0 only:
  validation, database/types, logger, resilience

Layer 2 — imports Layers 0–1:
  database, auth, event, storage, tenancy, worker

Layer 3 — imports Layers 0–2:
  binders, middlewares, datatable, resource, observability, web, audit, health

Layer 4 — imports Layers 0–3:
  bootstrap, frontend, go2ts

Layer 5 — example only:
  go-core-example
```

Verify after any import change with: go build ./...
An import cycle produces: "import cycle not allowed".

Specific rules:
  - auth     MUST NOT import database, bootstrap, or event
  - event    MUST NOT import bootstrap, auth, or database
  - audit    MUST NOT import bootstrap or auth
  - database MUST NOT import event, auth, or bootstrap
  - health   MUST NOT import bootstrap (bootstrap imports health, not the reverse)
  - resilience MUST NOT import anything from go-core (pure algorithms)

---

## 5. The New DI Container Design (Task 5.1)

### 5.1 Two Storage Maps

The new bootstrap.Container has exactly two internal storage maps:

```
direct    map[reflect.Type]any            — values registered via Bind[S]
providers map[reflect.Type]*providerInfo  — functions registered via Register()
```

These maps are NEVER mixed. Bind writes only to direct. Register writes only to
providers. There is no third map.

### 5.2 Resolution Order

resolveByType(typ reflect.Type) checks in this EXACT order:

  1. Check direct[typ]. If found, return it immediately. No lock needed beyond RLock.
  2. Check a lazy singleton cache instances[typ]. If found, return it.
  3. Look up providers[typ]. If not found, return "service not found" error.
  4. Resolve all deps recursively by calling resolveByType for each dep type.
  5. Call the provider function with the resolved dep values.
  6. Store the result in instances[typ] for future calls.
  7. Return the result.

### 5.3 What Bind[S] Does

Bind[S any](c *Container, val S):
  - Acquires write lock
  - Checks strict mode: if c.strict && direct[typ] already exists → panic
  - Stores val in c.direct[typ]
  - Does NOT touch c.providers

### 5.4 What Rebind[S] Does

Rebind[S any](c *Container, val S):
  - Same as Bind but skips the strict-mode duplicate check
  - Used ONLY for intentional overwrites (e.g., swapping InMemoryBus → NATSBus)
  - builder.go WithNATSBus, WithJWTAuth, WithDBTokenAuth must use Rebind

### 5.5 What Register() Does

Register(providerFn any) error:
  - Validates that providerFn is a func with signature: func(...deps) (T, error)
  - Stores in c.providers[retType]
  - Does NOT resolve anything yet (lazy)

### 5.6 What Build() Does

Build() error:
  - Reads c.providers (NOT c.direct — direct bindings have zero deps)
  - Builds an adjacency graph: for each provider, for each dep type, if that dep
    type also has a provider, add an edge
  - Runs DFS cycle detection on that graph
  - Returns nil if no cycle, or an error describing the cycle path

### 5.7 providerInfo Struct

```go
type providerInfo struct {
    fn   reflect.Value  // the provider function as a reflect.Value
    out  reflect.Type   // the return type T (key in providers map)
    deps []reflect.Type // the input types (dependencies)
}
```

deps is an empty slice for zero-argument providers. It is NEVER nil for safety.

### 5.8 Mutex Strategy

All reads from direct or providers use c.mu.RLock() / RUnlock().
All writes to direct, providers, or instances use c.mu.Lock() / Unlock().
When resolveByType upgrades from RLock to Lock (to write instances), it must
release RLock first, then acquire Lock, then re-check if the instance was added
by a concurrent goroutine (double-checked locking pattern):

  c.mu.RLock()
  if inst, ok := c.instances[typ]; ok { c.mu.RUnlock(); return inst, nil }
  c.mu.RUnlock()
  // ... resolve deps ...
  c.mu.Lock()
  if inst, ok := c.instances[typ]; ok { c.mu.Unlock(); return inst, nil } // re-check
  c.instances[typ] = inst
  c.mu.Unlock()

---

## 6. Health Package Design (Task 7.2)

After Task 7.2, the health infrastructure moves from bootstrap/health.go to a
new top-level go-core/health/ package. The dependency direction is:

  bootstrap → health    (bootstrap imports health)
  health    → database  (DBHealthChecker imports *gorm.DB)
  health    → apperr    (optional)

The health package exports:
  - Checker interface { Check(ctx context.Context) error }
  - Registry struct   { Add(Checker), SetDraining(bool), IsDraining() bool }
  - DBChecker struct  { NewDBChecker(db *gorm.DB) *DBChecker }
  - LivenessHandler(registry *Registry) gin.HandlerFunc
  - ReadinessHandler(registry *Registry) gin.HandlerFunc

bootstrap.App.Shutdown() and bootstrap.AppBuilder.setupHealth() resolve
*health.Registry from the container to call SetDraining(true).

---

## 7. Upload Sub-Package Design (Task 7.3)

After Task 7.3, file upload logic moves to go-core/web/upload/upload.go.

The upload package (package upload) exports:
  - Config struct  (was UploadConfig)
  - File struct    (was UploadedFile)
  - Upload(ctx *gin.Context, formKey string, cfg Config) (File, error)
  - Delete(basePath, relativePath string) error

web/helpers.go retains ONLY:
  - GetParamInt(ctx *gin.Context, key string) (int, bool)
  - GetQueryInt(ctx *gin.Context, key string) (int, bool)
  - GetPathID(ctx *gin.Context) (int, bool)

---

## 8. Error Typing Rules for web/helpers.go (Task 1.4)

UploadFile returns errors that reach the HTTP ErrorHandler. All must be *apperr.AppError:

  Client errors (HTTP 400):     apperr.BadRequest(message)
    - BaseDir is empty
    - File too large
    - MIME type not allowed
    - Path escapes upload directory
    - Directory traversal in DeleteFile

  System errors (HTTP 500):     apperr.Internal(err)
    - Failed to read file bytes
    - Failed to seek file
    - Failed to create directory
    - Failed to create destination file

  Not found (HTTP 404):         apperr.NotFound(message)
    - File does not exist in DeleteFile

fmt.Errorf is NOT acceptable for any of these paths.

---

## 9. NATS Error Handling Rules (Task 8.1)

After Task 8.1, NATSBus has a *slog.Logger field.
NewNATSBus(client NatsClient, log *slog.Logger) *NATSBus

In the Subscribe callback, errors are logged at Error level:
  - Unmarshal of Envelope fails AND fallback raw unmarshal also fails:
    log.Error("nats: failed to unmarshal message", "subject", subject, "error", err)
  - handler(ctx, recv) returns a non-nil error:
    log.Error("nats: handler returned error", "subject", subject, "error", err)

The handler error is logged but NOT propagated (NATS delivery is async; there
is no caller to propagate to). This is intentional and the comment must say so.

---

## 10. Testing Strategy

### 10.1 Test File Location and Package Name

Unit tests for foo.go live in foo_test.go in the same directory, same package name.
Example: database/registry.go → database/registry_test.go, package database.

### 10.2 No Real Network or Disk in Unit Tests

Unit tests must not open real TCP connections. Use:
  - database.PrepareTestDB() or database.NewTestRegistry() for SQLite in-memory
  - testhelpers.NewLocalTempDriver() for storage tests
  - testhelpers.NewInMemoryBus() for event tests
  - httptest.Server for HTTP client tests (OIDC provider, NATS envelope)

### 10.3 Race Detector

Any task that adds or modifies goroutines, channels, sync.Mutex, sync.RWMutex,
sync.Map, sync.Once, atomic, or singleflight MUST be verified with:
  go test -race ./path/to/package/...

### 10.4 Benchmarks

Phase 4 performance tasks must include at least one Benchmark* function.
Report the full output of:
  go test -bench=. -benchmem ./path/to/package/...

### 10.5 Test Function Naming

Descriptive. Follow the pattern:
  Test[Type]_[Method]_[Condition]_[ExpectedOutcome]
Examples:
  TestAsyncRepository_Write_AfterShutdown_ReturnsError
  TestContainer_Build_WithCycle_ReturnsError
  TestNATSBus_Subscribe_UnmarshalError_IsLogged

---

## 11. Package-Level Impact Map

This section lists exactly which files are touched by each phase. No file outside
this map should change unless a task description explicitly requires it.

### Phase 1 — Bug Fixes
- audit/async.go, audit/async_test.go
- bootstrap/builder.go, bootstrap/app.go, bootstrap/app_test.go
- web/helpers.go, web/helpers_test.go
- bootstrap/config_loader.go, bootstrap/config_loader_test.go
- event/envelope.go, event/nats_adapter.go, event/nats_envelope_test.go

### Phase 2 — Dead Code Removal
- binders/cache.go, binders/json.go (no test changes needed)
- internal/config/ (entire directory deleted)
- worker/worker.go_example (file deleted)
- auth/hasher.go, auth/rbac.go, auth/rbac_test.go
- database/migrator.go, database/migrator_safe_test.go

### Phase 3 — Duplicate Elimination
- database/testutil.go + database/testing.go → database/test_helpers.go
- database/testing_test.go → database/test_helpers_test.go
- auth/provider.go, auth/oidc_provider.go
- datatable/datatable.go

### Phase 4 — Performance
- resource/resource.go, resource/resource_test.go
- datatable/datatable.go
- worker/manager.go, worker/manager_test.go

### Phase 5 — DI Container Promotion
- bootstrap/container.go (rewritten)
- bootstrap/builder.go (Bind → Rebind for intentional overwrites)
- bootstrap/container_test.go (updated)
- internal/di/ (deleted after zero-reference grep)
- internal/di/di_test.go (deleted with the directory)

### Phase 6 — Naming and DX
- binders/json.go, middlewares/bind.go (renamed BindJSON → BindRequest)
- testhelpers/db.go and all callers (rename NewTestDB → NewTestRegistry)
- validation/validator.go (Register → MustRegister, add Register returning error)
- datatable/datatable.go (WithQuery → WithScope)
- datatable/result.go (add PageMeta() method)
- web/response.go (Paginated uses PageMeta())

### Phase 7 — Logic Relocation
- database/transactor.go (add NewTransactorFromRegistry)
- bootstrap/container.go (ResolveTransactor delegates to database)
- health/ (new package: health.go, handlers.go, health_test.go, handlers_test.go)
- bootstrap/health.go (deleted after content moved to health/)
- bootstrap/builder.go (imports health/ instead of inline)
- bootstrap/app.go (imports health/ for SetDraining)
- web/upload/ (new package: upload.go, upload_test.go)
- web/helpers.go (upload functions removed, only URL helpers remain)

### Phase 8 — Feature Additions
- event/nats_adapter.go (logger injection)
- event/nats_test.go (updated for new NewNATSBus signature)
- storage/driver.go (List + Exists added to interface)
- storage/local/local.go (List + Exists implemented)
- storage/local/local_test.go (new tests for List + Exists)
- audit/async.go (onError callback field)
- audit/async_test.go (test for onError invocation)
- bootstrap/app.go (parallel registerModules)
- bootstrap/app_test.go (test for parallel registration)

---

## 12. Forbidden Actions (All Phases)

1. Do not use error string matching (strings.Contains(err.Error(), "...")) to
   determine error type. Use errors.As, errors.Is, or type assertions.

2. Do not use fmt.Println, log.Print, log.Fatal, or log.Println in any
   non-test file.

3. Do not introduce new package-level var that is a pointer and is mutated after
   program start. Constants and zero-value structs are fine.

4. Do not call panic() outside a function named Must*.

5. Do not add any new entry to go.mod that is not already present.

6. Do not change database migration files (*.sql) or GORM AutoMigrate call lists
   unless a task explicitly requires it.

7. Do not change the Bus interface (Publish, Subscribe signatures).

8. Do not change the Module interface (Name, Register, Boot, Shutdown signatures).

9. Do not change the Transactor interface (WithinTransaction signature).

10. Do not change the storage.Driver interface except in Task 8.2, which is the
    one task explicitly permitted to extend it.

---

## 13. File Header Convention

Every new .go file starts with the package declaration on line 1, followed
immediately by the import block (if any imports are needed). No copyright headers.
No build tags unless the task explicitly requires them. No author or date comments.