# GO-CORE SECURITY & RELIABILITY FIX PLAN — v3

## Overview

This plan contains 40 atomic tasks across 6 phases, addressing the 22 blocking
issues and highest-impact warnings found in the April 2026 production-readiness
audit. Execute tasks in strict phase order. Do not begin Phase N+1 until every
task in Phase N is verified and explicitly approved.

Read AGENT.md and ARCHITECTURE.md completely before executing any task.

Module path : github.com/wssto2/go-core
Go version  : 1.25.1

---

## PHASE 1 — SECURITY

Five issues that allow attackers to read arbitrary files, spoof identities,
inject headers, exhaust memory, or forge tokens. Fix all five before any other
phase.

---

### Task 1.1 — Path traversal protection in `storage/local` [x]

**Problem**
Every method in `storage/local/local.go` constructs paths as
`filepath.Join(d.Root, key)` with no validation that the result stays inside
`d.Root`. A key like `../../etc/passwd` resolves outside the root directory.

**Files to read first**
1. `storage/local/local.go` — entire file
2. `apperr/errors.go` — confirm `BadRequest` signature

**Exact steps**
1. Add a private helper `func (d *LocalDriver) safePath(key string) (string, error)`:
   - Return `apperr.BadRequest` if `key == ""`.
   - Join: `p := filepath.Join(d.Root, filepath.Clean("/"+key))`
   - Guard: if `p+sep` does not start with `filepath.Clean(d.Root)+sep`, return
     `apperr.BadRequest("storage key escapes root directory")`.
2. Replace every direct `filepath.Join(d.Root, key)` in `Put`, `Get`, `Delete`,
   `URL`, `List`, and `Exists` with `d.safePath(key)`, propagating the error.
3. Add `"strings"` to the import block if absent.

**Test to write**
File: `storage/local/local_test.go`
- `TestLocalDriver_SafePath_RejectsTraversal`: confirm `../` key returns BadRequest.
- `TestLocalDriver_SafePath_RejectsEmptyKey`: confirm empty key returns BadRequest.

**Verification commands**
```
go build ./...
go test ./storage/local/...
go test ./...
go vet ./...
```

---

### Task 1.2 — Remove X-User-ID trust in rate-limit middleware [x]

**Problem**
`middlewares/ratelimit_middleware.go` falls back to the client-supplied
`X-User-ID` header. Any client can set `X-User-ID: admin` to exhaust another
user's quota or escape their own.

**Files to read first**
1. `middlewares/ratelimit_middleware.go` — entire file
2. `auth/context.go` — confirm `auth.UserFromContext` signature

**Exact steps**
1. Delete the `else if h := ctx.GetHeader("X-User-ID")...` branch entirely.
2. Fall back to `ctx.ClientIP()` when no authenticated user is present.
3. Add a one-line comment documenting the fallback.

**Test to write**
File: `middlewares/ratelimit_middleware_test.go`
- `TestRateLimitMiddleware_XUserIDHeaderIgnored`: send `X-User-ID: spoofed`,
  confirm rate-limit key is the client IP.

**Verification commands**
```
go build ./...
go test ./middlewares/...
go test ./...
go vet ./...
```

---

### Task 1.3 — Sanitise X-Request-ID before reflection [x]

**Problem**
`middlewares/request_id.go` reflects the raw `X-Request-ID` header into response
headers and logs without validation. A client can inject CRLF sequences for
header injection or arbitrarily long strings for log inflation.

**Files to read first**
1. `middlewares/request_id.go` — entire file

**Exact steps**
1. Add a package-level `var validRequestID = regexp.MustCompile("^[a-zA-Z0-9\-_]{1,128}$")`.
2. After reading the header, if `!validRequestID.MatchString(requestID)`, discard
   and generate a new UUID.

**Test to write**
File: `middlewares/request_id_test.go`
- CRLF-containing value is rejected and replaced with a UUID.
- Value longer than 128 chars is rejected.
- Valid UUID passes through unchanged.
- Empty value generates a new UUID.

**Verification commands**
```
go build ./...
go test ./middlewares/...
go test ./...
go vet ./...
```

---

### Task 1.4 — Limit request body size in JSON binder [x]

**Problem**
`binders/json.go` calls `io.ReadAll(r.Body)` with no size limit. A client can
stream a multi-GB body and exhaust server memory before any handler runs.

**Files to read first**
1. `binders/json.go` — entire file

**Exact steps**
1. Add `const maxBodyBytes int64 = 10 << 20` (10 MB) at package level.
2. Replace `io.ReadAll(r.Body)` with `io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))`.
3. After the read, if `int64(len(body)) > maxBodyBytes` return
   `apperr.BadRequest("request body too large")`.

**Test to write**
File: `binders/json_test.go`
- `TestBind_OversizedBody_ReturnsBadRequest`: body of `maxBodyBytes+1` bytes
  returns a BadRequest apperr.

**Verification commands**
```
go build ./...
go test ./binders/...
go test ./...
go vet ./...
```

---

### Task 1.5 — Require non-empty JWT secret at startup [x]

**Problem**
`bootstrap/config.go` has no `validate:"required"` tag on the JWT secret field.
An unset `JWT_SECRET` results in tokens signed with an empty string.

**Files to read first**
1. `bootstrap/config.go` — entire file
2. `bootstrap/builder.go` — search for `WithJWTAuth` to confirm consumption

**Exact steps**
1. Add `validate:"required"` to the JWT secret field.
2. In `WithJWTAuth`, add an explicit startup guard:
   ```
   if b.cfg.JWT.Secret == "" {
       panic("WithJWTAuth: JWT_SECRET must not be empty")
   }
   ```
   Panic is appropriate here per Rule I (Must-style startup guard).

**Test to write**
File: `bootstrap/config_test.go` (create if absent)
- `TestConfig_EmptyJWTSecret_FailsValidation`: `LoadConfig` returns an error
  when JWT secret is empty.

**Verification commands**
```
go build ./...
go test ./bootstrap/...
go test ./...
go vet ./...
```

---

## PHASE 2 — CRASH / PANIC

Six issues that cause nil-pointer dereferences, unchecked type assertions, or
sends on closed channels — all resulting in immediate process crashes.

---

### Task 2.1 — Fix send-on-closed-channel in `storage/pool` [x]

**Problem**
`storage/pool/pool.go` `Put()` checks `p.closed` under a mutex, releases the
mutex, then sends on the channel. A concurrent `Close()` between the two
operations panics: send on closed channel.

**Files to read first**
1. `storage/pool/pool.go` — entire file

**Exact steps**
1. Read both `Put` and the receive side to determine if holding the mutex during
   the send would deadlock. If safe, keep the mutex held across the check and send.
2. If holding the mutex would deadlock, add a `recover()` wrapper inside `Put`
   that catches the panic and returns the connection to the caller as an error.
3. Document the chosen strategy in a comment.

**Test to write**
File: `storage/pool/pool_test.go`
- `TestPool_Put_ConcurrentClose_NoPanic`: 50 goroutines each call `Put` and
  `Close` concurrently. Must pass `go test -race` with zero panics.

**Verification commands**
```
go build ./...
go test -race ./storage/pool/...
go test ./...
go vet ./...
```

---

### Task 2.2 — Fix unchecked type assertion in `utils.Pluck` [x]

**Problem**
`utils/helpers.go` `Pluck` uses `fieldVal.Interface().(K)` without the comma-ok
form, panicking on any type mismatch.

**Files to read first**
1. `utils/helpers.go` — read the `Pluck` function

**Exact steps**
1. Replace with: `if v, ok := fieldVal.Interface().(K); ok { *destination = append(*destination, v) }`

**Test to write**
File: `utils/helpers_test.go`
- `TestPluck_TypeMismatch_SkipsField`: struct with a non-matching field type does
  not panic and the field is silently skipped.

**Verification commands**
```
go build ./...
go test ./utils/...
go test ./...
go vet ./...
```

---

### Task 2.3 — Fix `utils.ToMap` panic on unexported fields [x]

**Problem**
`utils/helpers.go` `ToMap` calls `field.Interface()` on every struct field.
`reflect.Value.Interface()` panics for unexported fields.

**Files to read first**
1. `utils/helpers.go` — read the `ToMap` function

**Exact steps**
1. Before `Interface()`, add: `if !typ.Field(i).IsExported() { continue }`

**Test to write**
File: `utils/helpers_test.go`
- `TestToMap_UnexportedFields_NoPanic`: struct with both exported and unexported
  fields does not panic; only exported fields appear in the result.

**Verification commands**
```
go build ./...
go test ./utils/...
go test ./...
go vet ./...
```

---

### Task 2.4 — Guard nil logger in `event/outbox_worker` [x]

**Problem**
`event/outbox_worker.go` accepts a `*slog.Logger` that may be nil and calls
`w.log.Warn` / `w.log.Error` unconditionally, causing a nil-pointer panic.

**Files to read first**
1. `event/outbox_worker.go` — entire file

**Exact steps**
1. In `NewOutboxWorker`, if the provided logger is nil: `log = slog.Default()`.

**Test to write**
File: `event/outbox_worker_test.go` (create if absent)
- `TestNewOutboxWorker_NilLogger_UsesDefault`: passing nil logger does not panic
  and the stored logger is non-nil.

**Verification commands**
```
go build ./...
go test ./event/...
go test ./...
go vet ./...
```

---

### Task 2.5 — Guard nil logger in `frontend/spa` NoRoute handler [x]

**Problem**
`frontend/spa.go` line 119: the `NoRoute` closure calls `log.Info(...)` even
when `RegisterSPA` is called with a nil logger. Every 404 panics.

**Files to read first**
1. `frontend/spa.go` — entire file

**Exact steps**
1. In `RegisterSPA`, if the passed logger is nil: `log = slog.Default()`.
2. Verify no other logger dereferences in the file lack this guard.

**Test to write**
File: `frontend/spa_test.go` (create or append)
- `TestRegisterSPA_NilLogger_NoPanic`: call `RegisterSPA` with a nil logger and
  trigger the NoRoute handler; confirm no panic.

**Verification commands**
```
go build ./...
go test ./frontend/...
go test ./...
go vet ./...
```

---

### Task 2.6 — Fix Prometheus label mismatch in observability Gin middleware [x]

**Problem**
`observability/metrics/middleware.go` line 22 calls
`m.requestDuration.WithLabelValues(method, path, status)` — three values — but
`requestDuration` is registered with only two labels. Prometheus panics at runtime.

**Files to read first**
1. `observability/metrics/middleware.go` — entire file
2. `observability/metrics/metrics.go` — confirm `requestDuration` label names

**Exact steps**
1. Remove the extra `status` argument:
   `m.requestDuration.WithLabelValues(method, path).Observe(...)`
2. Add `requestErrors` recording for 5xx responses (missing from Gin middleware,
   present in net/http middleware — close the gap):
   `if status >= 500 { m.requestErrors.WithLabelValues(method, path, strconv.Itoa(status)).Inc() }`
3. Confirm `requestErrors` label names match the registration before writing.

**Test to write**
File: `observability/metrics/middleware_test.go` (create or append)
- `TestInstrumentHTTP_NoLabelMismatchPanic`: call `InstrumentHTTP` on a test
  handler, fire a request, assert no panic.

**Verification commands**
```
go build ./...
go test ./observability/...
go test ./...
go vet ./...
```

---

## PHASE 3 — RACE CONDITIONS

Three confirmed data races. All must pass `go test -race`.

---

### Task 3.1 — Fix WaitGroup race in `audit/async` [x]

**Problem**
`audit/async.go` `Write()` calls `wg.Add(1)` after `Shutdown()` may have already
called `wg.Wait()`. The Go runtime panics: *sync: WaitGroup.Add called
concurrently with Wait*.

**Files to read first**
1. `audit/async.go` — entire file

**Exact steps**
1. Add a `mu sync.Mutex` and `closed bool` field to `AsyncRepository`.
2. Protect the closed check and `wg.Add(1)` atomically under `mu`:
   ```
   a.mu.Lock()
   if a.closed { a.mu.Unlock(); return apperr.Internal(...) }
   a.wg.Add(1)
   a.mu.Unlock()
   ```
3. In `Shutdown()`, set `a.closed = true` under the same mutex before closing
   the channel, then call `a.wg.Wait()` after releasing it.
4. Remove any `atomic.Bool` added by prior work if it conflicts.

**Test to write**
File: `audit/async_test.go`
- `TestAsyncRepository_WriteAfterShutdown_NoRace`: 10 goroutines call `Write`
  and `Shutdown` concurrently; all post-shutdown writes return an error; no panic.
  Must pass `go test -race`.

**Verification commands**
```
go build ./...
go test -race ./audit/...
go test ./...
go vet ./...
```

---

### Task 3.2 — Fix broken `List()` in `storage/memory` [x]

**Problem**
`storage/memory/memory.go` `List()` uses `break` on `key > prefix` during map
iteration. Go map iteration is non-deterministic, so results are random and wrong.

**Files to read first**
1. `storage/memory/memory.go` — entire file

**Exact steps**
1. Replace the body of `List()` with a prefix-matching scan:
   ```
   for key := range d.data {
       if strings.HasPrefix(key, prefix) {
           keys = append(keys, key)
       }
   }
   sort.Strings(keys)
   ```
2. Add `"sort"` and `"strings"` to imports if absent.

**Test to write**
File: `storage/memory/memory_test.go`
- `TestMemoryDriver_List_ReturnsPrefixedKeys`: store keys `a/1`, `a/2`, `b/1`;
  `List("a/")` returns exactly `["a/1", "a/2"]` in sorted order on repeated calls.

**Verification commands**
```
go build ./...
go test ./storage/memory/...
go test ./...
go vet ./...
```

---

### Task 3.3 — Fix data race on `SpanRecord` in tracing [x]

**Problem**
`observability/tracing/tracing.go` `finish()` writes `rec.End` and `rec.Errored`
without holding the mutex. Concurrent `FinishedSpans()` reads the same slice.

**Files to read first**
1. `observability/tracing/tracing.go` — entire file

**Exact steps**
1. In the `finish` closure, acquire `t.mu.Lock()` before writing `rec.End` and
   `rec.Errored`, hold it through the `t.spans = append(...)` call, then unlock.
2. In `FinishedSpans()`, copy the slice under `t.mu.RLock()` and return the copy.

**Test to write**
File: `observability/tracing/tracing_test.go` (create or append)
- `TestSimpleTracer_ConcurrentFinish_NoRace`: 20 goroutines start spans and call
  `finish` concurrently while `FinishedSpans` is called from another goroutine.
  Must pass `go test -race`.

**Verification commands**
```
go build ./...
go test -race ./observability/...
go test ./...
go vet ./...
```

---

## PHASE 4 — CRITICAL RELIABILITY

Seven blocking issues causing silent data loss, resource leaks, infinite loops,
or deployment-breaking failures.

---

### Task 4.1 — Track and drain NATS subscriptions [x]

**Problem**
`event/nats_adapter.go` discards the `*nats.Subscription` returned by
`client.Subscribe`. There is no `Unsubscribe` path. Re-registering a subject
stacks duplicate handlers — every message is processed N times.

**Files to read first**
1. `event/nats_adapter.go` — entire file
2. `event/bus.go` — confirm the `Bus` interface

**Exact steps**
1. Add `subs map[string]NatsSubscription` and `mu sync.Mutex` to `NATSBus`.
   Define `NatsSubscription` as an interface with `Unsubscribe() error` so the
   type can be mocked in tests.
2. In `Subscribe`, if an existing sub for the subject exists, call
   `existing.Unsubscribe()` first. Store the new subscription.
3. Add `Close() error` method that unsubscribes all tracked subscriptions.
4. Wire `Close()` into the bootstrap shutdown path if `NATSBus` is registered.

**Test to write**
File: `event/nats_adapter_test.go` (create or append)
- `TestNATSBus_Resubscribe_NoDuplicateHandlers`: second `Subscribe` call for the
  same subject replaces the first; handler is invoked exactly once per message.

**Verification commands**
```
go build ./...
go test ./event/...
go test ./...
go vet ./...
```

---

### Task 4.2 — Add TTL cleanup for stale idempotency reservations [x]

**Problem**
`event/idempotency.go` `DBProcessedStore` inserts a row with `processed_at IS NULL`
when a message is reserved. A crash before `Confirm` leaves this row forever,
permanently blocking re-processing of that key.

**Files to read first**
1. `event/idempotency.go` — entire file
2. `event/processed.go` — entire file

**Exact steps**
1. Add a `reserved_at` timestamp column (or reuse `created_at`) to
   `IdempotencyRecord`.
2. In `Reserve`, set `reserved_at = time.Now()`.
3. Add `PurgeStaleReservations(ctx context.Context, olderThan time.Duration) error`
   to `DBProcessedStore`: deletes rows where
   `processed_at IS NULL AND reserved_at < NOW() - olderThan`.
4. For `InMemoryProcessedStore`, add a `reservationTTL` field and evict
   reserved-but-never-confirmed entries in `evictExpired`.

**Test to write**
File: `event/idempotency_test.go` (create or append)
- `TestDBProcessedStore_PurgeStaleReservations_RemovesOldEntries`: SQLite in-memory
  DB; stale rows deleted, fresh rows remain.

**Verification commands**
```
go build ./...
go test ./event/...
go test ./...
go vet ./...
```

---

### Task 4.3 — Dead-letter outbox events with empty EventType [x]

**Problem**
`event/outbox_worker.go` skips events with an empty `EventType` via `continue`
without marking them processed. They are re-fetched and re-skipped on every poll
tick forever, causing infinite log spam.

**Files to read first**
1. `event/outbox_worker.go` — entire file
2. The outbox repository — confirm the `MarkProcessed` (or equivalent) signature

**Exact steps**
1. Replace the `continue` with a dead-letter step:
   ```
   w.log.Error("outbox: dead-lettering event with empty type", "event_id", ev.ID)
   _ = w.db.MarkProcessed(ctx, ev.ID) // best-effort; intentionally ignored
   continue
   ```
2. If a dedicated dead-letter table/status exists, use it instead.

**Test to write**
File: `event/outbox_worker_test.go`
- `TestOutboxWorker_EmptyEventType_MarkedProcessed`: event with empty `EventType`
  is not re-fetched on subsequent polls.

**Verification commands**
```
go build ./...
go test ./event/...
go test ./...
go vet ./...
```

---

### Task 4.4 — Propagate HTTP server startup failure in `bootstrap/app` [x]

**Problem**
`bootstrap/app.go` starts the HTTP server in a goroutine and only logs errors.
A port-in-use error causes `Run()` to return `nil` while the app runs with no
HTTP server. `http.ErrServerClosed` is also incorrectly logged as a failure on
every clean shutdown.

**Files to read first**
1. `bootstrap/app.go` — entire file

**Exact steps**
1. Use an `errCh chan error` to capture early startup failures:
   ```
   errCh := make(chan error, 1)
   go func() {
       if err := a.httpServer.Start(); err != nil &&
          !errors.Is(err, http.ErrServerClosed) {
           errCh <- err
       }
   }()
   select {
   case err := <-errCh:
       return fmt.Errorf("http server failed to start: %w", err)
   case <-time.After(100 * time.Millisecond):
   }
   ```

**Test to write**
File: `bootstrap/app_test.go` (create or append)
- `TestApp_Run_PortInUse_ReturnsError`: start a listener on a port; call `Run`
  targeting the same port; assert non-nil error is returned.

**Verification commands**
```
go build ./...
go test ./bootstrap/...
go test ./...
go vet ./...
```

---

### Task 4.5 — Add `ReadHeaderTimeout` to HTTP server [x]

**Problem**
`bootstrap/builder.go` constructs `http.Server` without `ReadHeaderTimeout`.
Without it, an attacker holds connections open by sending headers one byte at a
time (Slowloris).

**Files to read first**
1. `bootstrap/builder.go` — search for `http.Server` construction
2. `bootstrap/config.go` — confirm the HTTP config struct

**Exact steps**
1. Add `ReadHeaderTimeoutSec int` to the HTTP config struct; default `10` in
   `DefaultConfig()`.
2. Set `ReadHeaderTimeout` in the `http.Server` literal.
3. If the value is zero after config load, default to `10 * time.Second`.

**Test to write**
File: `bootstrap/builder_test.go` (create or append)
- `TestWithHTTP_DefaultConfig_HasReadHeaderTimeout`: server struct has a
  non-zero `ReadHeaderTimeout`.

**Verification commands**
```
go build ./...
go test ./bootstrap/...
go test ./...
go vet ./...
```

---

### Task 4.6 — Fix `toJSON` template function to return `template.JS` [x]

**Problem**
`frontend/spa.go` `BuiltinFuncMap` `toJSON` returns a plain `string`. Inside a
`<script>` block, `html/template` JS-escapes the string, mangling JSON.
Developers work around this with `template.HTML`, bypassing XSS protection.

**Files to read first**
1. `frontend/spa.go` — read `BuiltinFuncMap` and `toJSON`

**Exact steps**
1. Change the return type to `template.JS` and handle the marshal error:
   ```
   "toJSON": func(v any) template.JS {
       b, err := json.Marshal(v)
       if err != nil { return template.JS("null") }
       return template.JS(b)
   },
   ```
2. Ensure `"html/template"` is imported (not `"text/template"`).

**Test to write**
File: `frontend/spa_test.go`
- `TestBuiltinFuncMap_ToJSON_ReturnsTemplateJS`: returned type is `template.JS`
  and a `<` character is NOT JS-escaped in the output.

**Verification commands**
```
go build ./...
go test ./frontend/...
go test ./...
go vet ./...
```

---

### Task 4.7 — Make OpenTelemetry exporter configurable [x]

**Problem**
`observability/tracing/otel.go` hardcodes a `stdouttrace` exporter. This is
development-only; production requires OTLP or a no-op exporter.

**Files to read first**
1. `observability/tracing/otel.go` — entire file
2. `go.mod` — confirm which OTel packages are available

**Exact steps**
1. Add `ExporterType string` to the tracing config (values: `"stdout"`, `"noop"`,
   `"otlp-grpc"`, `"otlp-http"`).
2. Switch on the value in `InitOpenTelemetry`. Default to `"noop"` when empty.
3. Only use packages already present in `go.mod`. If OTLP exporters are absent,
   implement `"stdout"` and `"noop"` only, and leave a TODO comment for OTLP.

**Test to write**
File: `observability/tracing/otel_test.go` (create or append)
- `TestInitOpenTelemetry_NoopExporter_NoError`
- `TestInitOpenTelemetry_StdoutExporter_NoError`

**Verification commands**
```
go build ./...
go test ./observability/...
go test ./...
go vet ./...
```

---

## PHASE 5 — HIGH-PRIORITY WARNINGS

Nine issues causing silent correctness failures, unbounded resource growth, or
information disclosure.

---

### Task 5.1 — Fix circuit breaker stuck in HALF-OPEN on panic [x]

**Problem**
`resilience/circuitbreaker.go` resets `trialInProgress` only when `runTrial`
returns normally. A panicking `op` leaves it `true` forever, rejecting all future
trial calls.

**Files to read first**
1. `resilience/circuitbreaker.go` — entire file

**Exact steps**
1. Wrap the `trialInProgress = false` reset in a `defer` at the top of
   `runTrial` so it fires even on panic.
2. Adjust lock ordering if the defer conflicts with existing mutex usage.

**Test to write**
File: `resilience/circuitbreaker_test.go`
- `TestCircuitBreaker_PanicInOp_ResetsTrial`: open breaker, move to HALF-OPEN,
  supply a panicking `op` (recover in test), confirm next trial call is accepted.

**Verification commands**
```
go build ./...
go test ./resilience/...
go test ./...
go vet ./...
```

---

### Task 5.2 — Cap `InMemoryDLQ` size [x]

**Problem**
`event/reliable_bus.go` `InMemoryDLQ` grows without bound. Under sustained
failures, every failed publish appends to the DLQ, causing OOM.

**Files to read first**
1. `event/reliable_bus.go` — entire file

**Exact steps**
1. Add `maxSize int` and `dropped int64` fields. Constructor: `NewInMemoryDLQ(maxSize int)`.
2. In `Enqueue`, when `len(d.entries) >= d.maxSize`, drop the oldest entry and
   increment `dropped`.
3. Accept a `*slog.Logger` in the constructor; log a warning when an entry is
   dropped.
4. Expose `Dropped() int64`.

**Test to write**
File: `event/reliable_bus_test.go` (create or append)
- `TestInMemoryDLQ_Overflow_DropsOldest`: after `maxSize`, oldest entry is
  evicted and `Dropped()` is incremented.

**Verification commands**
```
go build ./...
go test ./event/...
go test ./...
go vet ./...
```

---

### Task 5.3 — Sanitise health check error responses [x]

**Problem**
`health/health.go` returns `"down: " + err.Error()` in the HTTP readiness
response. Database errors can include connection strings and credentials.

**Files to read first**
1. `health/health.go` — entire file

**Exact steps**
1. Change the response value to the string `"down"`.
2. Log the full error internally:
   `h.log.ErrorContext(ctx, "health: checker failed", "checker", checker.Name(), "error", err)`
3. Ensure `HealthRegistry` has a `*slog.Logger` field.

**Test to write**
File: `health/health_test.go`
- `TestReadinessHandler_CheckerError_DoesNotLeakDetails`: failing checker
  produces body containing `"down"` but NOT the internal error message.

**Verification commands**
```
go build ./...
go test ./health/...
go test ./...
go vet ./...
```

---

### Task 5.4 — Add per-check timeout to DB health checker [x]

**Problem**
`health/health.go` passes the raw request context to `PingContext`. If the
database hangs, the Kubernetes probe blocks until its own timeout fires, causing
spurious pod restarts.

**Files to read first**
1. `health/health.go` — read the DB checker

**Exact steps**
1. In `Check`, wrap the context:
   ```
   pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
   defer cancel()
   return d.db.PingContext(pingCtx)
   ```
2. Make the timeout a configurable option on the checker (default: 3s).

**Test to write**
File: `health/health_test.go`
- `TestDBHealthChecker_SlowPing_TimesOut`: mock DB blocks for 10s; assert check
  returns within 4s with a deadline-exceeded error.

**Verification commands**
```
go build ./...
go test ./health/...
go test ./...
go vet ./...
```

---

### Task 5.5 — Implement empty validation rules [x]

**Problem**
`validation/rules.go` `MinRule`, `MaxRule`, `InRule`, `DateRule`, and
`DateTimeRule` have empty `Validate` bodies — they silently pass all values.

**Files to read first**
1. `validation/rules.go` — entire file
2. `validation/validator.go` — how rules receive their parameter
3. `validation/errors.go` — error construction pattern

**Exact steps**
1. `MinRule`: parse param as int; fail if field value (int cast or string length) < min.
2. `MaxRule`: same, upper bound.
3. `InRule`: split param on `|`; fail if value not in set.
4. `DateRule`: parse field as `time.DateOnly` (`2006-01-02`); fail if invalid.
5. `DateTimeRule`: parse as `time.RFC3339`; fail if invalid.
6. Malformed rule parameter → `apperr.Internal` (programmer error).

**Test to write**
File: `validation/rules_test.go` (create or append)
Table-driven tests for each rule: valid input passes, invalid input returns
a validation error, malformed parameter returns internal error.

**Verification commands**
```
go build ./...
go test ./validation/...
go test ./...
go vet ./...
```

---

### Task 5.6 — Evict expired entries in `InMemoryLimiter` [x]

**Problem**
`ratelimit/ratelimit.go` `InMemoryLimiter` never removes expired windows.
Under IP-based limiting with rotating IPs, the map grows indefinitely.

**Files to read first**
1. `ratelimit/ratelimit.go` — entire file

**Exact steps**
1. In `Allow`, delete a map entry if its window has expired before re-inserting.
2. Start a background goroutine in the constructor that sweeps the map every
   `windowSize`, deleting expired entries.
3. Expose `Stop()` to terminate the goroutine.

**Test to write**
File: `ratelimit/ratelimit_test.go` (create or append)
- `TestInMemoryLimiter_ExpiredEntries_AreEvicted`: after window expiry, map size
  returns to zero and `Stop()` terminates cleanly. Must pass `go test -race`.

**Verification commands**
```
go build ./...
go test -race ./ratelimit/...
go test ./...
go vet ./...
```

---

### Task 5.7 — Add exponential backoff to worker manager restart loop [x]

**Problem**
`worker/manager.go` restarts failing workers with a fixed 1-second delay and no
restart cap, generating ~60 restarts/minute for broken workers forever.

**Files to read first**
1. `worker/manager.go` — entire file

**Exact steps**
1. Track `restarts int` per worker goroutine.
2. Backoff: `min(initialDelay * 2^restarts, 60s)` with ±25% jitter via `math/rand`.
3. Reset `restarts` to 0 when a worker runs successfully for > 30s.
4. Add `MaxRestarts int` config (default 0 = unlimited). When non-zero, stop
   after `MaxRestarts` consecutive failures and log an error.

**Test to write**
File: `worker/manager_test.go` (create or append)
- `TestManager_MaxRestarts_StopsAfterLimit`: always-erroring worker is stopped
  after `MaxRestarts`.

**Verification commands**
```
go build ./...
go test ./worker/...
go test ./...
go vet ./...
```

---

### Task 5.8 — Fix UTF-8 truncation in `utils.StringClean` [x]

**Problem**
`utils/helpers.go` `StringClean` truncates at a byte offset (`str[:limit]`),
which cuts multi-byte UTF-8 sequences and produces invalid UTF-8.

**Files to read first**
1. `utils/helpers.go` — read `StringClean`

**Exact steps**
1. Convert to `[]rune` before truncating:
   ```
   runes := []rune(str)
   if len(runes) > limit { runes = runes[:limit] }
   return strings.TrimSpace(string(runes))
   ```

**Test to write**
File: `utils/helpers_test.go`
- `TestStringClean_MultiByte_NoCorruption`: string with multi-byte characters
  (e.g., Japanese) is truncated at rune boundaries and output is valid UTF-8.

**Verification commands**
```
go build ./...
go test ./utils/...
go test ./...
go vet ./...
```

---

### Task 5.9 — Fix MySQL-only backtick quoting in tenancy scope [x]

**Problem**
`tenancy/scope.go` uses backtick column quoting, which is MySQL-specific and
breaks on PostgreSQL.

**Files to read first**
1. `tenancy/scope.go` — entire file
2. A GORM usage file to confirm `db.Statement.Quote` availability

**Exact steps**
1. Replace `fmt.Sprintf("`%s` = ?", column)` with GORM portable quoting using
   `db.Statement.Quote(column) + " = ?"`. If a `*gorm.DB` is available in scope,
   use it directly; otherwise accept it as a parameter.
2. Apply to both `ScopeByTenant` and `RequireTenantScope`.

**Test to write**
File: `tenancy/scope_test.go` (create or append)
- `TestScopeByTenant_PostgreSQLDialect_NoBackticks`: mock GORM DB with a
  PostgreSQL-style quoter; no backticks in generated SQL.

**Verification commands**
```
go build ./...
go test ./tenancy/...
go test ./...
go vet ./...
```

---

## PHASE 6 — REMAINING WARNINGS

Ten issues causing subtle bugs, silent errors, security misconfigurations, or
poor developer experience.

---

### Task 6.1 — Fix idempotency middleware orphaned entry on handler panic [x]

**Problem**
`middlewares/idempotency.go` creates a store entry before `ctx.Next()`. If the
handler panics, `store.setResponse` is never called, the channel stays open
forever, and all future requests with the same key block indefinitely.

**Files to read first**
1. `middlewares/idempotency.go` — entire file

**Exact steps**
1. Wrap the `ctx.Next()` + `store.setResponse` calls in a deferred function:
   ```
   defer func() {
       store.setResponse(key, cw.Status(), cw.Header(), cw.Body())
   }()
   ctx.Next()
   ```
2. Ensure `cw` (response writer wrapper) is initialised before the defer.

**Test to write**
File: `middlewares/idempotency_test.go` (create or append)
- `TestIdempotencyMiddleware_HandlerPanic_ChannelClosed`: second request with the
  same key after a handler panic does not block indefinitely.

**Verification commands**
```
go build ./...
go test ./middlewares/...
go test ./...
go vet ./...
```

---

### Task 6.2 — Cap idempotency key length [x]

**Problem**
`middlewares/idempotency.go` accepts keys of unbounded length from the header.
A 1 MB key exhausts memory and inflates log storage.

**Files to read first**
1. `middlewares/idempotency.go` — entire file

**Exact steps**
1. After reading the header, if `len(key) > 256`, abort with HTTP 400:
   `ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key too long (max 256 bytes)"})`

**Test to write**
File: `middlewares/idempotency_test.go`
- `TestIdempotencyMiddleware_LongKey_Returns400`

**Verification commands**
```
go build ./...
go test ./middlewares/...
go test ./...
go vet ./...
```

---

### Task 6.3 — Fix `web.Fail` to abort the handler chain [x]

**Problem**
`web/handle.go` `Fail()` calls `ctx.Error(err)` but does NOT call `ctx.Abort()`.
Callers who expect it to halt execution will continue processing downstream
handlers silently.

**Files to read first**
1. `web/handle.go` — entire file
2. Run: `grep -r "web.Fail\|handle.Fail" . --include="*.go"` to find all callers

**Exact steps**
1. Add `ctx.Abort()` inside `Fail` after `ctx.Error(err)`.
2. Review each call site found by grep and remove any now-redundant manual
   `return` or `ctx.Abort()` that was compensating for the old behaviour.

**Test to write**
File: `web/handle_test.go` (create or append)
- `TestFail_AbortsHandlerChain`: after `Fail`, a subsequent handler in the chain
  is not executed.

**Verification commands**
```
go build ./...
go test ./web/...
go test ./...
go vet ./...
```

---

### Task 6.4 — Fix Chrome/Edge User-Agent detection order [x]

**Problem**
`web/ua.go` checks for `Chrome` before `Edg/`. Edge's UA contains both strings,
so Edge is always misidentified as Chrome.

**Files to read first**
1. `web/ua.go` — entire file

**Exact steps**
1. Move the Edge pattern (`Edg/`) before the Chrome pattern in
   `compiledBrowserPatterns`.

**Test to write**
File: `web/ua_test.go` (create or append)
- `TestGetUserAgentBrowser_EdgeUA_ReturnsEdge`: real-world Edge UA string returns
  `"Edge"` not `"Chrome"`.

**Verification commands**
```
go build ./...
go test ./web/...
go test ./...
go vet ./...
```

---

### Task 6.5 — Change default CORS from wildcard to deny-all [x]

**Problem**
`engine/gin.go` `withDefaults()` sets `AllowOrigins: []string{"*"}`. The default
engine accepts cross-origin requests from any domain without explicit config.

**Files to read first**
1. `engine/gin.go` — entire file
2. `go-core-example/` — confirm no example relies on the wildcard default

**Exact steps**
1. Change to `AllowOrigins: []string{}`.
2. Update README or inline doc if it describes the default CORS behaviour.

**Test to write**
File: `engine/gin_test.go` (create or append)
- `TestWithDefaults_CORS_NoWildcard`: default config has an empty AllowOrigins.

**Verification commands**
```
go build ./...
go test ./engine/...
go test ./...
go vet ./...
```

---

### Task 6.6 — Guard HSTS header to HTTPS responses only [x]

**Problem**
`middlewares/security.go` sets `Strict-Transport-Security` on every response
regardless of HTTPS. HSTS on plain HTTP is a protocol violation.

**Files to read first**
1. `middlewares/security.go` — entire file

**Exact steps**
1. Wrap the HSTS header in an HTTPS check:
   ```
   if ctx.Request.TLS != nil ||
      (cfg.TrustProxy && ctx.GetHeader("X-Forwarded-Proto") == "https") {
       ctx.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
   }
   ```
2. Add `TrustProxy bool` to the security config (default: false).

**Test to write**
File: `middlewares/security_test.go` (create or append)
- `TestSecurityMiddleware_HTTP_NoHSTSHeader`
- `TestSecurityMiddleware_HTTPS_HSTSPresent` (with `TrustProxy: true` and
  `X-Forwarded-Proto: https`)

**Verification commands**
```
go build ./...
go test ./middlewares/...
go test ./...
go vet ./...
```

---

### Task 6.7 — Clean up partial upload file on `io.Copy` failure [x]

**Problem**
`web/upload/upload.go` deferred cleanup checks the `os.Create` error, not the
`io.Copy` error. A failed copy leaves a partial file on disk. Additionally,
`UploadedFile.Size` reports the client-claimed size, not actual bytes written.

**Files to read first**
1. `web/upload/upload.go` — entire file

**Exact steps**
1. In the `io.Copy` error branch, explicitly clean up:
   ```
   if copyErr != nil {
       _ = dst.Close()
       _ = os.Remove(fullPath) // best-effort; intentionally ignored
       return UploadedFile{}, apperr.Internal(copyErr)
   }
   ```
2. Change `Size: header.Size` to `Size: written` in the returned `UploadedFile`.

**Test to write**
File: `web/upload/upload_test.go` (create or append)
- `TestUpload_CopyFailure_NoPartialFile`: mock reader fails mid-copy; destination
  file does not exist afterward.
- `TestUpload_Size_IsActualBytes`: `UploadedFile.Size` equals actual bytes written.

**Verification commands**
```
go build ./...
go test ./web/...
go test ./...
go vet ./...
```

---

### Task 6.8 — Use `errors.As` in `apperr.GetHTTPStatus` [x]

**Problem**
`apperr/http.go` uses a direct type assertion `err.(*AppError)` which does not
unwrap errors created with `fmt.Errorf("%w", appErr)`.

**Files to read first**
1. `apperr/http.go` — entire file

**Exact steps**
1. Replace with `errors.As`:
   ```
   var ae *AppError
   if errors.As(err, &ae) { return ae.HTTPStatus() }
   return http.StatusInternalServerError
   ```

**Test to write**
File: `apperr/http_test.go` (create or append)
- `TestGetHTTPStatus_WrappedAppError_Unwraps`: `fmt.Errorf("ctx: %w", apperr.NotFound("x"))`
  returns 404.

**Verification commands**
```
go build ./...
go test ./apperr/...
go test ./...
go vet ./...
```

---

### Task 6.9 — Fix unchecked type assertion in `logger.GetFromContext` [x]

**Problem**
`logger/logger.go` `GetFromContext` uses a bare type assertion `val.(*slog.Logger)`
which panics if the context value is a different type.

**Files to read first**
1. `logger/logger.go` — read `GetFromContext`

**Exact steps**
1. Use the comma-ok form:
   ```
   if l, ok := val.(*slog.Logger); ok { return l }
   return slog.Default()
   ```

**Test to write**
File: `logger/logger_test.go` (create or append)
- `TestGetFromContext_WrongType_ReturnsDefault`: integer stored in context key;
  `GetFromContext` returns default logger without panicking.

**Verification commands**
```
go build ./...
go test ./logger/...
go test ./...
go vet ./...
```

---

### Task 6.10 — Fix `reflectioncache` returning shared backing array [x]

**Problem**
`internal/reflectioncache/cache.go` returns the cached `[]FieldInfo` slice
directly. Callers that sort or append can corrupt the cache for all future callers.

**Files to read first**
1. `internal/reflectioncache/cache.go` — entire file

**Exact steps**
1. In both the cache-hit and fresh-entry return paths, return a copy:
   ```
   result := make([]FieldInfo, len(fields))
   copy(result, fields)
   return result
   ```

**Test to write**
File: `internal/reflectioncache/cache_test.go` (create or append)
- `TestCache_ReturnedSlice_IsIndependent`: sorting the first returned slice does
  not affect the order returned by a second call for the same type.

**Verification commands**
```
go build ./...
go test ./internal/...
go test ./...
go vet ./...
```

---

## Phase Completion Markers

* [x] Phase 1 — Security               (Tasks 1.1–1.5)
* [x] Phase 2 — Crash / Panic          (Tasks 2.1–2.6)
* [x] Phase 3 — Race Conditions        (Tasks 3.1–3.3)
* [x] Phase 4 — Critical Reliability   (Tasks 4.1–4.7)
* [x] Phase 5 — High-Priority Warnings (Tasks 5.1–5.9)
* [x] Phase 6 — Remaining Warnings     (Tasks 6.1–6.10)


---

## PHASE 7 — LATE-AUDIT BLOCKING ISSUES (Addenda)

Five additional blocking issues found in the final review pass. Triage and
execute these before declaring the full plan complete.

---

### Task 7.1 — Fix `database/types` driver.Value returning `int` not `int64` [x]

**Problem**
`database/types/int.go` and `null_int.go` implement `driver.Valuer` but return
`int` instead of `int64`. The `database/sql/driver` specification requires
`int64`. pgx and other strict drivers panic when scanning these values.

**Files to read first**
1. `database/types/int.go` — entire file
2. `database/types/null_int.go` — entire file

**Exact steps**
1. In both files, change the `Value()` return from `int` to `int64`:
   `return int64(v), nil`

**Test to write**
File: `database/types/int_test.go` (create or append)
- `TestIntType_Value_ReturnsInt64`: call `Value()` and assert the returned
  `driver.Value` is of type `int64`.

**Verification commands**
```
go build ./...
go test ./database/types/...
go test ./...
go vet ./...
```

---

### Task 7.2 — Fix refresh token replay via missing transaction [x]

**Problem**
`auth/refresh.go` `RotateRefreshToken` does a find-then-delete-then-insert
without a transaction or `SELECT ... FOR UPDATE`. Two concurrent requests with
the same refresh token can both succeed, allowing token replay.

**Files to read first**
1. `auth/refresh.go` — entire file
2. `auth/gormstore/token_store.go` — entire file
3. `database/transactor.go` — confirm `Transactor` interface

**Exact steps**
1. Wrap the find, delete, and insert operations in a single DB transaction.
2. Use `SELECT ... FOR UPDATE` (or equivalent pessimistic lock) when finding the
   token so a second concurrent request blocks until the first commits or rolls back.
3. If the token is not found within the transaction, return an appropriate error.

**Test to write**
File: `auth/refresh_test.go` (create or append)
- `TestRotateRefreshToken_ConcurrentRequests_OnlyOneSucceeds`: 10 goroutines
  call `RotateRefreshToken` with the same token concurrently; assert exactly one
  succeeds and the rest receive a not-found or conflict error.

**Verification commands**
```
go build ./...
go test -race ./auth/...
go test ./...
go vet ./...
```

---

### Task 7.3 — Fix JWKS fetch using request context in singleflight [x]

**Problem**
`auth/oidc_provider.go` calls `singleflight.Do` with the individual HTTP request
context. If the triggering client disconnects, the shared in-flight JWKS fetch
is cancelled, and ALL goroutines waiting on the same key receive a cancellation
error — causing a mass auth failure for all concurrent requests.

**Files to read first**
1. `auth/oidc_provider.go` — entire file

**Exact steps**
1. Replace the request context passed to `singleflight.Do` with a server-lifetime
   context (or `context.Background()`) plus an explicit timeout:
   ```
   fetchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
   defer cancel()
   result, err, _ = sf.Do(key, func() (any, error) {
       return fetchJWKS(fetchCtx, url)
   })
   ```
2. The timeout value should be configurable via the OIDC provider config.

**Test to write**
File: `auth/oidc_provider_test.go` (create or append)
- `TestOIDCProvider_ClientDisconnect_DoesNotCancelOtherRequests`: simulate a
  client context cancellation; confirm other concurrent auth requests still
  receive a valid JWKS response.

**Verification commands**
```
go build ./...
go test ./auth/...
go test ./...
go vet ./...
```

---

### Task 7.4 — Validate JWT audience claim [x]

**Problem**
`auth/provider.go` `resolveFromClaims` validates the issuer but ignores the
`aud` claim. A token issued for `service-a` is silently accepted by `service-b`.

**Files to read first**
1. `auth/provider.go` — entire file
2. `go.mod` — confirm jwt library version (`github.com/golang-jwt/jwt/v5`)

**Exact steps**
1. Add an `Audience []string` field to the JWT provider config.
2. When parsing tokens, pass `jwt.WithAudience(cfg.Audience...)` to the parse
   options if `Audience` is non-empty.
3. Document that leaving `Audience` empty disables audience validation (with a
   warning comment).

**Test to write**
File: `auth/provider_test.go` (create or append)
- `TestJWTProvider_WrongAudience_ReturnsUnauthorized`: token with `aud: service-b`
  is rejected by a provider configured for `aud: service-a`.

**Verification commands**
```
go build ./...
go test ./auth/...
go test ./...
go vet ./...
```

---

### Task 7.5 — Fix goroutine leak in `DBTokenProvider.Verify` [x]

**Problem**
`auth/provider.go` `DBTokenProvider.Verify` spawns a fire-and-forget goroutine
on every authenticated request. Under load this creates unbounded goroutine
growth with no backpressure, lifecycle management, or error visibility.

**Files to read first**
1. `auth/provider.go` — read `DBTokenProvider.Verify`
2. `worker/pool.go` — confirm `Submit` signature and `ErrQueueFull`

**Exact steps**
1. If the background work is low-priority (e.g., updating last-seen timestamp),
   submit it to the existing worker pool instead of spawning a raw goroutine.
2. If no pool is available in scope, accept a `worker.Pool` in the
   `DBTokenProvider` constructor.
3. Handle `worker.ErrQueueFull` by logging a warning and skipping the background
   work — it is non-critical.

**Test to write**
File: `auth/provider_test.go` (create or append)
- `TestDBTokenProvider_Verify_NoGoroutineLeak`: call `Verify` 1000 times rapidly
  and confirm the goroutine count does not grow unboundedly (use
  `runtime.NumGoroutine()` before and after with a reasonable delta).

**Verification commands**
```
go build ./...
go test ./auth/...
go test ./...
go vet ./...
```

---

* [x] Phase 7 — Late-Audit Blocking Issues (Tasks 7.1–7.5)
