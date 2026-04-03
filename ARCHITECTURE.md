# GO-CORE ARCHITECTURE — v3 Security & Reliability Fix Guide

## 1. Purpose of This Document

This document describes the engineering principles, package dependency rules,
security patterns, and fix constraints that govern the v3 work defined in
PLAN.md. Every task in PLAN.md must conform to the rules here. When a task
description and this document conflict, this document wins.

Re-read this document at the start of every new phase.

---

## 2. What This Work Covers

The April 2026 production-readiness audit identified 22 blocking issues across
six categories. This plan addresses all of them plus the highest-impact warnings.

| Phase | Focus | Blocking count |
|-------|-------|---------------|
| 1 | Security | 5 |
| 2 | Crash / Panic | 6 |
| 3 | Race Conditions | 3 |
| 4 | Critical Reliability | 7 |
| 5 | High-Priority Warnings | 9 |
| 6 | Remaining Warnings | 10 |

Additional blocking issues found late in the audit and tracked as Phase 4 addenda:
- `database/types`: `driver.Value()` returns `int` not `int64` (pgx panics)
- `auth/refresh.go`: refresh token rotation has no transaction / SELECT FOR UPDATE
- `auth/oidc_provider.go`: JWKS fetch propagates request context cancellation
  through singleflight, causing mass auth failures on client disconnect

---

## 3. Non-Negotiable Engineering Principles

### 3.1 Read Before You Write

Before modifying any file, read the ENTIRE file. Import paths, struct field
names, function signatures, and constant names must be verified from the live
file — not from memory or prior conversation summaries.

### 3.2 One Task at a Time

Each PLAN.md task is atomic. Do not combine tasks. Do not start Task N+1 before
Task N has a passing test and explicit user approval.

### 3.3 No Logic Deletions Without Verified Zero References

Before deleting any exported symbol, run:
  grep -r "SymbolName" . --include="*.go"
Only delete when the command returns zero lines outside the owning file.

### 3.4 Zero Warnings

After every task, both of these commands must exit 0 and print nothing:
  go build ./...
  go vet ./...

### 3.5 Every Code Change Requires a Test

Every task that modifies production code must add or update a `_test.go` file.
The test must assert the specific behaviour the task fixes. Tests live in the
same directory as the production code, same package name (or `_test` suffix for
black-box tests only).

### 3.6 Exact Module Path

The Go module path is: `github.com/wssto2/go-core`
Verify in `go.mod` before writing any import block.

### 3.7 Use apperr for Cross-Package Errors

Errors that cross a package boundary or reach the HTTP ErrorHandler must be
`*apperr.AppError`. Use:
  apperr.BadRequest(message)      — invalid client input
  apperr.NotFound(message)        — missing resource
  apperr.Internal(err)            — unexpected system failure
  apperr.Wrap(err, message, code) — wrap with new context

`fmt.Errorf` is acceptable only for sentinel errors consumed within the same
package and never reaching the HTTP layer.

### 3.8 Never Silently Drop Errors

`_ = someFunc()` is forbidden without a comment explaining exactly why the
error is safe to ignore in that specific context.

---

## 4. Package Dependency Rules (INVARIANT)

These import relationships must hold after every task. A violation means an
architecture error was introduced.

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

Specific prohibitions:
- `auth`     MUST NOT import `database`, `bootstrap`, or `event`
- `event`    MUST NOT import `bootstrap`, `auth`, or `database`
- `audit`    MUST NOT import `bootstrap` or `auth`
- `database` MUST NOT import `event`, `auth`, or `bootstrap`
- `health`   MUST NOT import `bootstrap`
- `resilience` MUST NOT import anything from go-core (pure algorithms)

Verify with: `go build ./...` — an import cycle produces "import cycle not allowed".

---

## 5. Security Fix Patterns

### 5.1 Path Traversal Prevention (Task 1.1)

All storage key validation must use the following canonical pattern. There are
no acceptable shortcuts.

```go
func safePath(root, key string) (string, error) {
    if key == "" {
        return "", apperr.BadRequest("storage key must not be empty")
    }
    // Clean("/"+key) normalises traversal sequences before join
    p := filepath.Join(root, filepath.Clean("/"+key))
    // Require the resolved path to be a child of root
    rootClean := filepath.Clean(root) + string(os.PathSeparator)
    if !strings.HasPrefix(p+string(os.PathSeparator), rootClean) {
        return "", apperr.BadRequest("storage key escapes root directory")
    }
    return p, nil
}
```

Apply to: `storage/local/local.go` — all six methods.

### 5.2 Untrusted Header Sanitisation (Tasks 1.2, 1.3)

The rule: **any client-controlled value that is written into a response header,
log field, or used for access control must be validated before use.**

For X-Request-ID reflection:
```go
var validRequestID = regexp.MustCompile(`^[a-zA-Z0-9\-_]{1,128}$`)
```
If the header fails validation, discard it and generate a UUID. Never truncate
or sanitise in-place — generate fresh.

For rate-limit identity: never use `X-User-ID` or similar client headers.
Identity for rate-limiting must come from the authenticated principal in context
(`auth.UserFromContext`) or from the network layer (`ctx.ClientIP()`).

### 5.3 Request Body Size Limits (Task 1.4)

All paths that call `io.ReadAll(r.Body)` must be wrapped:
```go
const maxBodyBytes int64 = 10 << 20 // 10 MB
body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
if err != nil { ... }
if int64(len(body)) > maxBodyBytes {
    return apperr.BadRequest("request body too large")
}
```

The +1 trick lets us distinguish "exactly at limit" from "over limit" without
a separate stat call.

### 5.4 JWT Secrets (Task 1.5)

- The `JWT.Secret` field must carry `validate:"required"`.
- `WithJWTAuth` must guard against the empty string at startup (panic is
  appropriate — it is a programmer error, not a runtime condition).
- Audience validation (`aud` claim) must be checked wherever JWT tokens are
  verified. Use the `jwt-go/v5` `WithAudience` parse option.

### 5.5 Template Safety (Task 4.6)

Functions in a `template.FuncMap` that output into `<script>` blocks must return
`template.JS` (from `html/template`), not `string`. Returning `string` causes
the template engine to JS-escape the output. Developers who encounter mangled
JSON inevitably work around it with `template.HTML`, which bypasses all XSS
protection.

Rule: if the function is named `toJSON`, `toJS`, or similar, its return type is
`template.JS`. Period.

---

## 6. Concurrency Fix Patterns

### 6.1 Channel Close Race (Tasks 2.1, 3.1)

The canonical pattern for a guarded channel send where the channel may be closed
concurrently:

**Option A — keep the mutex held across both the check and the send:**
```go
p.mu.Lock()
if p.closed {
    p.mu.Unlock()
    return ErrClosed
}
select {
case p.ch <- item:
    p.mu.Unlock()
    return nil
default:
    p.mu.Unlock()
    return ErrFull
}
```
Use this when the send is non-blocking (`default` branch exists) so holding
the mutex cannot deadlock.

**Option B — recover from the panic:**
```go
func safeSend(ch chan<- T, item T) (panicked bool) {
    defer func() {
        if r := recover(); r != nil { panicked = true }
    }()
    ch <- item
    return false
}
```
Use this only when holding the mutex across the send would deadlock (e.g.,
the receiver also acquires the same mutex).

**WaitGroup race pattern (Task 3.1):** The `wg.Add(1)` and the closed check
must be atomic under the same mutex. There is no safe way to do this with
atomics alone when `wg.Wait()` is called from a different goroutine.

### 6.2 Struct Field Data Race (Task 3.3)

Struct fields written by a `finish` closure and read by a concurrent
`FinishedSpans()` call must be protected by the same mutex. The correct pattern:

```go
finish := func(err error) {
    t.mu.Lock()
    rec.End = time.Now()        // write under lock
    rec.Errored = err != nil    // write under lock
    t.spans = append(t.spans, rec)
    t.mu.Unlock()
}
```

```go
func (t *T) FinishedSpans() []SpanRecord {
    t.mu.RLock()
    result := make([]SpanRecord, len(t.spans))
    copy(result, t.spans)   // copy under lock
    t.mu.RUnlock()
    return result           // return copy, not slice alias
}
```

### 6.3 singleflight and Context Propagation (Auth OIDC)

When using `singleflight.Group`, the context passed to the shared call must NOT
be the individual request context. If it is, a single client disconnect cancels
the in-flight fetch for all goroutines waiting on the same key.

Pattern: use `context.Background()` (or a server-lifecycle context) for the
shared fetch, and apply a separate timeout:
```go
fetchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
result, err, _ = sf.Do(key, func() (any, error) {
    return fetchJWKS(fetchCtx, url)
})
```

---

## 7. Reliability Fix Patterns

### 7.1 Deferred Resource Release (Task 6.1)

Any resource that MUST be released when a handler exits — including channel
writes that unblock waiting requests — must be in a `defer`, not after
`ctx.Next()`:

```go
// WRONG — skipped if handler panics:
ctx.Next()
store.setResponse(key, status, header, body)

// CORRECT:
defer func() {
    store.setResponse(key, cw.Status(), cw.Header(), cw.Body())
}()
ctx.Next()
```

### 7.2 Partial File Cleanup (Task 6.7)

Any code path that creates a file and fails before completing the write MUST
remove the partial file:

```go
written, copyErr := io.Copy(dst, src)
if copyErr != nil {
    _ = dst.Close()
    _ = os.Remove(fullPath) // best-effort cleanup of partial file
    return apperr.Internal(copyErr)
}
```

The `_ =` on `os.Remove` is intentional and must be commented. The original
error takes precedence; a remove failure at this point is secondary.

### 7.3 Infinite Retry Loop Prevention (Task 4.3)

Any worker that skips a queue item with `continue` must also advance the item's
state so it is not re-fetched on the next poll. The canonical dead-letter pattern:

```go
if ev.EventType == "" {
    log.Error("outbox: dead-lettering event", "event_id", ev.ID)
    _ = repo.MarkProcessed(ctx, ev.ID) // best-effort; intentionally ignored
    continue
}
```

### 7.4 Circuit Breaker Panic Safety (Task 5.1)

State that guards re-entry into a half-open circuit breaker must be reset in a
`defer` to ensure it is cleared even when the probed operation panics:

```go
func (cb *CircuitBreaker) runTrial(ctx context.Context, op func(context.Context) error) error {
    defer func() {
        cb.mu.Lock()
        cb.trialInProgress = false
        cb.mu.Unlock()
    }()
    return op(ctx)
}
```

---

## 8. Observability Fix Patterns

### 8.1 Prometheus Label Cardinality (Task 2.6)

The number of `WithLabelValues(...)` arguments must EXACTLY match the number of
label names in the metric's `Opts`. This is not caught at compile time — only at
runtime (panic).

Before writing `WithLabelValues(a, b, c)`, read the metric registration and
count the label names. There is no exception.

### 8.2 HTTP Error Spans (Task 4.7 / OBS-03)

A tracing middleware must record span errors for HTTP 5xx responses. This
requires a `statusRecorder` wrapper around the response writer:

```go
type statusRecorder struct {
    gin.ResponseWriter
    status int
}
func (r *statusRecorder) WriteHeader(code int) {
    r.status = code
    r.ResponseWriter.WriteHeader(code)
}
```

Pass the recorded status to `finish` after `ctx.Next()`.

---

## 9. Package-Level Impact Map for v3

Only files listed here should change unless a task description explicitly
requires a change outside this map.

```
Phase 1 — Security
  storage/local/local.go, storage/local/local_test.go
  middlewares/ratelimit_middleware.go, middlewares/ratelimit_middleware_test.go
  middlewares/request_id.go, middlewares/request_id_test.go
  binders/json.go, binders/json_test.go
  bootstrap/config.go, bootstrap/builder.go, bootstrap/config_test.go

Phase 2 — Crash / Panic
  storage/pool/pool.go, storage/pool/pool_test.go
  utils/helpers.go, utils/helpers_test.go
  event/outbox_worker.go, event/outbox_worker_test.go
  frontend/spa.go, frontend/spa_test.go
  observability/metrics/middleware.go, observability/metrics/middleware_test.go
  observability/metrics/metrics.go

Phase 3 — Race Conditions
  audit/async.go, audit/async_test.go
  storage/memory/memory.go, storage/memory/memory_test.go
  observability/tracing/tracing.go, observability/tracing/tracing_test.go

Phase 4 — Critical Reliability
  event/nats_adapter.go, event/nats_adapter_test.go
  event/idempotency.go, event/idempotency_test.go
  event/processed.go
  event/outbox_worker.go, event/outbox_worker_test.go
  bootstrap/app.go, bootstrap/app_test.go
  bootstrap/builder.go, bootstrap/builder_test.go
  bootstrap/config.go
  frontend/spa.go, frontend/spa_test.go
  observability/tracing/otel.go, observability/tracing/otel_test.go

Phase 5 — High-Priority Warnings
  resilience/circuitbreaker.go, resilience/circuitbreaker_test.go
  event/reliable_bus.go, event/reliable_bus_test.go
  health/health.go, health/health_test.go
  validation/rules.go, validation/rules_test.go
  ratelimit/ratelimit.go, ratelimit/ratelimit_test.go
  worker/manager.go, worker/manager_test.go
  utils/helpers.go, utils/helpers_test.go
  tenancy/scope.go, tenancy/scope_test.go

Phase 6 — Remaining Warnings
  middlewares/idempotency.go, middlewares/idempotency_test.go
  web/handle.go, web/handle_test.go
  web/ua.go, web/ua_test.go
  engine/gin.go, engine/gin_test.go
  middlewares/security.go, middlewares/security_test.go
  web/upload/upload.go, web/upload/upload_test.go
  apperr/http.go, apperr/http_test.go
  logger/logger.go, logger/logger_test.go
  internal/reflectioncache/cache.go, internal/reflectioncache/cache_test.go
```

---

## 10. Additional Blocking Issues (Post-Audit Addenda)

The following issues were found in the final code-review pass and are NOT yet
in PLAN.md phases 1–6. They should be triaged into a Phase 7 before those tasks
are declared complete.

### A1 — `database/types` driver.Value returns `int` not `int64`
File: `database/types/int.go:25`, `database/types/null_int.go:26`
The `database/sql/driver.Value` interface requires `int64` for integers.
Returning a plain `int` causes pgx (and some other drivers) to panic at scan
time. Fix: cast to `int64` in `Value()`.

### A2 — Refresh token rotation has no transaction
File: `auth/refresh.go:24`, `auth/gormstore/token_store.go:31`
`RotateRefreshToken` does a find-then-update without a transaction or
`SELECT ... FOR UPDATE`. Two concurrent requests with the same refresh token
can both succeed (token replay). Fix: wrap find+delete+insert in a single DB
transaction with a row-level lock.

### A3 — JWKS fetch propagates cancellation through singleflight
File: `auth/oidc_provider.go:110`
`singleflight.Do` is called with the HTTP request context. If the triggering
client disconnects, the context is cancelled and ALL goroutines waiting on the
same singleflight key receive a cancellation error, causing a mass auth failure.
Fix: use `context.Background()` with an explicit timeout for the shared fetch
(see Section 6.3).

### A4 — JWT audience never validated
File: `auth/provider.go:70`
`resolveFromClaims` checks issuer but ignores the `aud` claim. A token issued
for service A is silently accepted by service B. Fix: pass expected audience(s)
to the JWT parse options (`jwt.WithAudience`).

### A5 — `auth/provider.go` goroutine leak
File: `auth/provider.go:113`
`DBTokenProvider.Verify` spawns a fire-and-forget goroutine on every
authenticated request with no lifecycle, backpressure, or error handling.
Under load this creates unbounded goroutine growth.
Fix: use a worker pool or move the background work to an existing worker manager.

---

## 11. Forbidden Actions (All Phases)

1. Do not use error string matching (`strings.Contains(err.Error(), "...")`) to
   determine error type. Use `errors.As`, `errors.Is`, or typed assertions.

2. Do not use `fmt.Println`, `log.Print`, `log.Fatal`, or `log.Println` in any
   non-test file.

3. Do not introduce new package-level `var` that is a pointer mutated after
   program start. Constants and zero-value structs are fine.

4. Do not call `panic()` outside a function named `Must*`.

5. Do not add any new entry to `go.mod`.

6. Do not change the `Bus` interface (Publish, Subscribe signatures).

7. Do not change the `Module` interface (Name, Register, Boot, Shutdown).

8. Do not change the `Transactor` interface (WithinTransaction signature).

9. Do not trust any HTTP header supplied by an unauthenticated client for
   identity, rate-limit bucketing, or access control decisions.

10. Do not return `string` from a `template.FuncMap` function that outputs
    into a `<script>` block or JS event handler attribute.

---

## 12. File Header Convention

Every new `.go` file starts with the package declaration on line 1, followed
immediately by the import block (if any imports are needed). No copyright
headers. No build tags unless a task explicitly requires them. No author or
date comments.
