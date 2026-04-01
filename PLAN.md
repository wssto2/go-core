# GO-CORE REFACTORING PLAN — v2

## Overview

This plan contains 31 atomic tasks across 8 phases, addressing bugs, dead code,
duplicate logic, performance, naming, DX, and architecture issues found in the
v2 code review. Execute tasks in strict phase order. Do not begin Phase N+1
until every task in Phase N is verified and explicitly approved by the user.

Read AGENT.md and ARCHITECTURE.md completely before executing any task.

The module path is: github.com/wssto2/go-core
The Go version is: 1.25.1

---

## PHASE 1 — CRITICAL BUG FIXES

---

### Task 1.1 — Fix Write-after-Shutdown panic in `audit/async.go` [x]

**Problem**
After `Shutdown()` calls `close(a.ch)`, any subsequent call to `Write()` reaches
the `case a.ch <- entry:` branch inside the select statement and panics with
"send on closed channel". There is no guard preventing this.

**Files to read first**
1. `go-core/audit/async.go` — read the ENTIRE file
2. `go-core/apperr/errors.go` — read lines 1–50 to verify the New() constructor
   signature: `func New(err error, message string, code Code) *AppError`

**Exact steps**
1. Open `go-core/audit/async.go`.
2. Add `"sync/atomic"` to the import block. The existing imports are `"context"`,
   `"sync"`, and `"github.com/wssto2/go-core/apperr"`. The new import block is:
   ```
   import (
       "context"
       "sync"
       "sync/atomic"
       "github.com/wssto2/go-core/apperr"
   )
   ```
3. Add a `closed atomic.Bool` field to the `AsyncRepository` struct. Place it
   as the last field, after `workers int`:
   ```
   type AsyncRepository struct {
       underlying Repository
       ch         chan Entry
       wg         sync.WaitGroup
       closeOnce  sync.Once
       workers    int
       closed     atomic.Bool
   }
   ```
4. In the `Shutdown()` method, add `a.closed.Store(true)` as the VERY FIRST
   line of the function body, BEFORE the `a.closeOnce.Do(...)` call:
   ```
   func (a *AsyncRepository) Shutdown(ctx context.Context) error {
       a.closed.Store(true)                          // ADD THIS LINE FIRST
       a.closeOnce.Do(func() { close(a.ch) })
       ...
   ```
5. In the `Write()` method, add the following as the VERY FIRST block inside
   the function body, before `a.wg.Add(1)`:
   ```
   if a.closed.Load() {
       return apperr.New(nil, "audit queue closed", apperr.CodeInternal)
   }
   ```
   The complete Write method after the change:
   ```
   func (a *AsyncRepository) Write(ctx context.Context, entry Entry) error {
       if a.closed.Load() {
           return apperr.New(nil, "audit queue closed", apperr.CodeInternal)
       }
       a.wg.Add(1)
       select {
       case a.ch <- entry:
           return nil
       default:
           a.wg.Done()
           return apperr.New(nil, "audit queue full", apperr.CodeInternal)
       }
   }
   ```
6. Do NOT change `loop()`, `NewAsyncRepository()`, or any other method.

**Test to write**
File: `go-core/audit/async_test.go`
Read the file first. Append the following function at the end of the file:

```go
func TestAsyncRepository_WriteAfterShutdown_ReturnsError(t *testing.T) {
    fr := &fakeRepo{}
    ar := NewAsyncRepository(fr, 10, 1)

    ctx := context.Background()
    shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()
    if err := ar.Shutdown(shutdownCtx); err != nil {
        t.Fatalf("Shutdown failed: %v", err)
    }

    err := ar.Write(ctx, NewEntry("user", 1, 1, "create"))
    if err == nil {
        t.Fatal("expected error from Write after Shutdown, got nil")
    }
}
```

**Verification commands**
```
go build ./...
go test -race ./audit/...
go test ./...
go vet ./...
```

---

### Task 1.2 — Fix double module registration in `bootstrap/builder.go` + `bootstrap/app.go` [x]

**Problem**
`AppBuilder.Build()` calls `mod.Register(b.container)` for every module, then
`App.Run()` calls `registerModules()` which calls `mod.Register(a.container)` a
second time. Modules are registered twice. With strict mode enabled the second
call will panic. Even without strict mode, side effects in Register run twice.

**Files to read first**
1. `go-core/bootstrap/builder.go` — read the ENTIRE file, especially Build()
2. `go-core/bootstrap/app.go` — read the ENTIRE file, especially Run() and
   registerModules()
3. `go-core/bootstrap/app_test.go` — read the ENTIRE file

**Exact steps**
1. Open `go-core/bootstrap/builder.go`.
2. Find the `Build()` method. It contains a for loop that registers modules:
   ```
   for _, mod := range b.modules {
       if err := mod.Register(b.container); err != nil {
           return nil, fmt.Errorf("bootstrap: module %q register: %w", mod.Name(), err)
       }
   }
   ```
3. DELETE that entire for loop from `Build()`. Do not change anything else in
   `Build()`. The method should still check `b.errors`, still call `NewApp(...)`,
   and still return the result. Only the registration loop is removed.
4. Do NOT touch `bootstrap/app.go`. The `registerModules()` call in `Run()` is
   correct and should remain as the single place registration happens.

**Test to write**
File: `go-core/bootstrap/app_test.go`
Read the file first. At the end of the file, add a `countingModule` helper type
and test function. The existing test file likely already defines mock modules —
read it to understand what names are already taken before adding new ones.

```go
type countingModule struct {
    name          string
    registerCount int
    bootCount     int
}

func (m *countingModule) Name() string { return m.name }
func (m *countingModule) Register(_ *Container) error {
    m.registerCount++
    return nil
}
func (m *countingModule) Boot(_ context.Context) error {
    m.bootCount++
    return nil
}
func (m *countingModule) Shutdown(_ context.Context) error { return nil }

func TestAppBuilder_ModuleRegisteredExactlyOnce(t *testing.T) {
    m := &countingModule{name: "counting"}

    cfg := DefaultConfig()
    builder := New(cfg)
    app, err := builder.WithModules(m).Build()
    if err != nil {
        t.Fatalf("Build() failed: %v", err)
    }
    // Build() must NOT register. registerCount must still be 0.
    if m.registerCount != 0 {
        t.Fatalf("Build() registered module %d time(s), want 0", m.registerCount)
    }

    ctx, cancel := context.WithCancel(context.Background())
    cancel() // cancel immediately so Run() shuts down fast

    // Run() calls registerModules() then bootModules().
    // Since ctx is already cancelled, it should register once and then stop.
    _ = app.Run()

    if m.registerCount != 1 {
        t.Fatalf("Run() registered module %d time(s), want 1", m.registerCount)
    }
}
```

Note: `New(cfg)` calls `DefaultInfrastructure()` implicitly in some codebases.
Read builder.go to see whether `New` calls `DefaultInfrastructure`. If it does
not, you may need to call `builder.DefaultInfrastructure()` before `WithModules`.
Read the file and follow what it actually does.

**Verification commands**
```
go build ./...
go test ./bootstrap/...
go test ./...
go vet ./...
```

---

### Task 1.3 — Fix `bootModules` passes `context.Background()` instead of errgroup context [x]

**Problem**
`bootModules` creates an errgroup with a derived cancellable context `gCtx`, but
immediately discards it by passing `context.Background()` to every module's
`Boot()` call. If one module fails, the others are never cancelled. The comment
explaining this says "MockModule compatibility" — this is a test workaround that
was baked into production code.

**Files to read first**
1. `go-core/bootstrap/app.go` — read the ENTIRE file, focus on bootModules()
2. `go-core/bootstrap/app_test.go` — read the ENTIRE file to understand mock modules

**Exact steps**
1. Open `go-core/bootstrap/app.go`.
2. Find `bootModules`. The current code is:
   ```
   func (a *App) bootModules(ctx context.Context) error {
       g, _ := errgroup.WithContext(ctx)
       for _, m := range a.modules {
           m := m
           g.Go(func() error {
               // Always call Boot with context.Background() for MockModule compatibility in tests
               if err := m.Boot(context.Background()); err != nil {
                   return fmt.Errorf("module %q boot failed: %w", m.Name(), err)
               }
               return nil
           })
       }
       return g.Wait()
   }
   ```
3. Change `g, _ := errgroup.WithContext(ctx)` to capture the derived context:
   ```
   g, gCtx := errgroup.WithContext(ctx)
   ```
4. Change `m.Boot(context.Background())` to `m.Boot(gCtx)`.
5. Remove the comment `// Always call Boot with context.Background() for MockModule compatibility in tests`.
6. The final result:
   ```
   func (a *App) bootModules(ctx context.Context) error {
       g, gCtx := errgroup.WithContext(ctx)
       for _, m := range a.modules {
           m := m
           g.Go(func() error {
               if err := m.Boot(gCtx); err != nil {
                   return fmt.Errorf("module %q boot failed: %w", m.Name(), err)
               }
               return nil
           })
       }
       return g.Wait()
   }
   ```
7. Read `app_test.go`. If any mock module's `Boot(ctx context.Context) error`
   method does something that breaks with a real context (e.g., uses a nil check
   on ctx), fix it. The `Module` interface requires `Boot(ctx context.Context) error`
   so every mock already accepts a context parameter. No change should be needed.

**Test to write**
File: `go-core/bootstrap/app_test.go`
Read the file first. Append this test:

```go
func TestBootModules_CancelsOthersOnFirstFailure(t *testing.T) {
    // failModule returns an error from Boot immediately.
    failModule := &mockModule{
        name: "fail",
        bootFn: func(ctx context.Context) error {
            return fmt.Errorf("intentional boot failure")
        },
    }
    // blockModule blocks until its context is cancelled.
    blockedCh := make(chan struct{})
    blockModule := &mockModule{
        name: "block",
        bootFn: func(ctx context.Context) error {
            select {
            case <-ctx.Done():
                close(blockedCh)
                return ctx.Err()
            case <-time.After(5 * time.Second):
                return fmt.Errorf("blockModule was not cancelled within 5s")
            }
        },
    }

    app := NewApp(DefaultConfig(), NewContainer(), nil, nil,
        []Module{failModule, blockModule})

    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()

    err := app.bootModules(ctx)
    if err == nil {
        t.Fatal("expected bootModules to return error, got nil")
    }

    select {
    case <-blockedCh:
        // blockModule received cancellation — correct
    case <-time.After(2 * time.Second):
        t.Fatal("blockModule was not cancelled within 2s — context not propagated")
    }
}
```

Note: If `mockModule` is already defined in `app_test.go`, use that definition
or a similar struct. Read the test file first to check what already exists. If
`bootFn` field does not exist on the existing mock, create a new type named
`failingModule` for this test instead. Do NOT duplicate existing type names.

**Verification commands**
```
go build ./...
go test ./bootstrap/...
go test ./...
go vet ./...
```

---

### Task 1.4 — Fix `web/helpers.go` upload errors use `fmt.Errorf` instead of `apperr` [x]

**Problem**
`UploadFile` and `DeleteFile` return errors created with `fmt.Errorf`. The HTTP
`ErrorHandler` middleware wraps unknown error types as `apperr.Internal` (HTTP 500).
Client errors — wrong MIME type, file too large, path traversal — incorrectly
return HTTP 500 instead of HTTP 400/404.

**Files to read first**
1. `go-core/web/helpers.go` — read the ENTIRE file
2. `go-core/apperr/errors.go` — read lines 1–80 to confirm exact signatures:
   - `func BadRequest(message string) *AppError`
   - `func NotFound(message string) *AppError`
   - `func Internal(err error) *AppError`

**Exact steps**
1. Open `go-core/web/helpers.go`.
2. Verify that `"github.com/wssto2/go-core/apperr"` is already in the import
   block. It is — do not add it again.
3. Replace every `fmt.Errorf` in `UploadFile` and `DeleteFile` according to
   this mapping. Find each line by its content and replace it:

   LINE: `return UploadedFile{}, fmt.Errorf("upload configuration error: BaseDir is required")`
   REPLACE WITH: `return UploadedFile{}, apperr.BadRequest("upload configuration error: BaseDir is required")`

   LINE: `return UploadedFile{}, fmt.Errorf("file size %d exceeds limit of %d bytes", header.Size, maxBytes)`
   REPLACE WITH: `return UploadedFile{}, apperr.BadRequest(fmt.Sprintf("file size exceeds %dMB limit", limitMB))`

   LINE: `return UploadedFile{}, fmt.Errorf("failed to read file: %w", err)`
   REPLACE WITH: `return UploadedFile{}, apperr.Internal(err)`

   LINE: `return UploadedFile{}, fmt.Errorf("failed to seek file: %w", err)`
   REPLACE WITH: `return UploadedFile{}, apperr.Internal(err)`

   LINE: `return UploadedFile{}, fmt.Errorf("file type '%s' is not allowed", sniffed)`
   REPLACE WITH: `return UploadedFile{}, apperr.BadRequest(fmt.Sprintf("file type '%s' is not allowed", sniffed))`

   LINE: `return UploadedFile{}, fmt.Errorf("failed to create upload directory: %w", err)`
   REPLACE WITH: `return UploadedFile{}, apperr.Internal(err)`

   LINE: `return UploadedFile{}, fmt.Errorf("upload: resolved path escapes upload directory")`
   REPLACE WITH: `return UploadedFile{}, apperr.BadRequest("upload: resolved path escapes upload directory")`

   LINE (inside the copy section, checking written bytes):
   `return UploadedFile{}, fmt.Errorf("file exceeds %d MB limit", limitMB)`
   REPLACE WITH: `return UploadedFile{}, apperr.BadRequest(fmt.Sprintf("file exceeds %d MB limit", limitMB))`

   For the `dst, err := os.Create(fullPath)` error path, read the file to see
   what the existing error return looks like. If it is a bare `return UploadedFile{}, err`,
   change it to `return UploadedFile{}, apperr.Internal(err)`.

   For `io.Copy` error (copyErr variable): change to `return UploadedFile{}, apperr.Internal(copyErr)`.

4. In `DeleteFile`:
   LINE: `return fmt.Errorf("invalid file path: directory traversal detected")`
   REPLACE WITH: `return apperr.BadRequest("invalid file path: directory traversal detected")`

   LINE: `return fmt.Errorf("file not found: %s", cleanFull)`
   REPLACE WITH: `return apperr.NotFound("file not found")`

5. After all replacements, check whether `"fmt"` is still used in the file
   (it is used in `fmt.Sprintf` calls). Keep the `"fmt"` import if Sprintf is
   still used. Remove it only if it is completely unused after your edits.

**Test to write**
File: `go-core/web/helpers_test.go`
If the file does not exist, create it with `package web`.
If it exists, read it first, then append.

```go
func TestUploadFile_OversizeFile_ReturnsBadRequestAppError(t *testing.T) {
    dir := t.TempDir()

    // Build a multipart request with content larger than 1MB limit
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)
    part, _ := writer.CreateFormFile("file", "big.txt")
    // Write 2MB of data
    chunk := make([]byte, 1024)
    for i := 0; i < 2048; i++ {
        part.Write(chunk)
    }
    writer.Close()

    req := httptest.NewRequest(http.MethodPost, "/", body)
    req.Header.Set("Content-Type", writer.FormDataContentType())
    w := httptest.NewRecorder()
    ctx, _ := gin.CreateTestContext(w)
    ctx.Request = req

    cfg := UploadConfig{
        BaseDir: dir,
        MaxSize: 1, // 1MB
    }
    _, err := UploadFile(ctx, "file", cfg)
    if err == nil {
        t.Fatal("expected error for oversized file, got nil")
    }
    var appErr *apperr.AppError
    if !errors.As(err, &appErr) {
        t.Fatalf("expected *apperr.AppError, got %T: %v", err, err)
    }
    if appErr.Code != apperr.CodeBadRequest {
        t.Fatalf("expected CodeBadRequest, got %s", appErr.Code)
    }
}
```

Add the required imports at the top of the test file:
```go
import (
    "bytes"
    "errors"
    "mime/multipart"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/gin-gonic/gin"
    "github.com/wssto2/go-core/apperr"
)
```

**Verification commands**
```
go build ./...
go test ./web/...
go test ./...
go vet ./...
```

---

### Task 1.5 — Fix `bootstrap/config_loader.go` `setField` missing float and uint types [x]

**Problem**
The `setField` function only handles `reflect.String`, `reflect.Int`,
`reflect.Bool`, and `reflect.Int64`. Config struct fields of type `float32`,
`float64`, `uint`, `uint8`, `uint16`, `uint32`, or `uint64` cause a
"unsupported type" error when loaded from environment variables.

**Files to read first**
1. `go-core/bootstrap/config_loader.go` — read the ENTIRE file
2. `go-core/bootstrap/config_loader_test.go` — read the ENTIRE file

**Exact steps**
1. Open `go-core/bootstrap/config_loader.go`.
2. Find the `setField` function. Its `switch field.Kind()` block ends with a
   `default:` case that returns `fmt.Errorf("unsupported type: %s", field.Kind())`.
3. Add two new `case` blocks IMMEDIATELY BEFORE the `default:` case:
   ```
   case reflect.Float32, reflect.Float64:
       f, err := strconv.ParseFloat(val, 64)
       if err != nil {
           return err
       }
       field.SetFloat(f)
   case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
       u, err := strconv.ParseUint(val, 10, 64)
       if err != nil {
           return err
       }
       field.SetUint(u)
   ```
4. Verify that `"strconv"` is already in the import block. It is — do not add
   it again.
5. Do NOT change any other part of setField or any other function.

**Test to write**
File: `go-core/bootstrap/config_loader_test.go`
Read the file first. Append a new test function at the end:

```go
func TestLoadConfig_FloatAndUintFields(t *testing.T) {
    type TestCfg struct {
        Score   float64 `env:"TEST_SCORE"`
        Count   uint    `env:"TEST_COUNT"`
        Rate    float32 `env:"TEST_RATE"`
        Limit   uint32  `env:"TEST_LIMIT"`
    }

    t.Setenv("TEST_SCORE", "3.14")
    t.Setenv("TEST_COUNT", "42")
    t.Setenv("TEST_RATE", "1.5")
    t.Setenv("TEST_LIMIT", "100")

    var cfg TestCfg
    if err := LoadConfig(&cfg); err != nil {
        t.Fatalf("LoadConfig failed: %v", err)
    }
    if cfg.Score != 3.14 {
        t.Errorf("Score: got %v, want 3.14", cfg.Score)
    }
    if cfg.Count != 42 {
        t.Errorf("Count: got %v, want 42", cfg.Count)
    }
    if cfg.Rate != 1.5 {
        t.Errorf("Rate: got %v, want 1.5", cfg.Rate)
    }
    if cfg.Limit != 100 {
        t.Errorf("Limit: got %v, want 100", cfg.Limit)
    }
}
```

**Verification commands**
```
go build ./...
go test ./bootstrap/...
go test ./...
go vet ./...
```

---

### Task 1.6 — Fix fragile Envelope discriminator in `event/nats_adapter.go` [x]

**Problem**
The Subscribe handler detects whether a received NATS message is an Envelope by
calling `json.Unmarshal(data, &env)` and checking `len(env.Payload) > 0`.
This succeeds for ANY valid JSON that has a `payload` key, not just Envelopes.
Any event struct with a `payload` JSON field is misidentified as an Envelope.

**Files to read first**
1. `go-core/event/envelope.go` — read the ENTIRE file
2. `go-core/event/nats_adapter.go` — read the ENTIRE file
3. `go-core/event/nats_envelope_test.go` — read the ENTIRE file

**Exact steps**
1. Open `go-core/event/envelope.go`.
2. Add a `Version string` field with json tag `"_v"` to the Envelope struct.
   Place it as the FIRST field:
   ```
   type Envelope struct {
       Version   string          `json:"_v"`
       RequestID string          `json:"request_id"`
       Timestamp time.Time       `json:"timestamp"`
       Source    string          `json:"source"`
       Payload   json.RawMessage `json:"payload"`
   }
   ```
3. Find the `WrapEventWithMetadata` function in `envelope.go`. It returns an
   Envelope. Add `Version: "1"` to the struct literal inside that function:
   ```
   return Envelope{
       Version:   "1",
       RequestID: ...,
       Timestamp: ...,
       Source:    ...,
       Payload:   ...,
   }, nil
   ```
   Read the actual function body first to insert correctly.
4. Open `go-core/event/nats_adapter.go`.
5. Find the Subscribe handler's discriminator check. The current line is:
   ```
   if err := json.Unmarshal(data, &env); err == nil && len(env.Payload) > 0 {
   ```
6. Change it to:
   ```
   if err := json.Unmarshal(data, &env); err == nil && env.Version == "1" && len(env.Payload) > 0 {
   ```
7. Do NOT change anything else in nats_adapter.go.

**Test to write**
File: `go-core/event/nats_envelope_test.go`
Read the existing file first. Append at the end:

```go
func TestNATSBus_PayloadFieldDoesNotTriggerEnvelopePath(t *testing.T) {
    // This struct has a "payload" json field but is NOT an Envelope.
    // Before the fix, this would be misidentified as an Envelope.
    type EventWithPayload struct {
        ID      int    `json:"id"`
        Payload string `json:"payload"`
    }

    received := make(chan EventWithPayload, 1)
    fake := &fakeNatsClient{}

    bus := NewNATSBus(fake)
    err := bus.Subscribe(EventWithPayload{}, func(ctx context.Context, ev any) error {
        if e, ok := ev.(EventWithPayload); ok {
            received <- e
        }
        return nil
    })
    if err != nil {
        t.Fatalf("Subscribe: %v", err)
    }

    // Publish raw JSON directly (bypassing Envelope wrapping).
    raw, _ := json.Marshal(EventWithPayload{ID: 99, Payload: "hello"})
    fake.deliver(raw)

    select {
    case e := <-received:
        if e.ID != 99 || e.Payload != "hello" {
            t.Fatalf("received wrong event: %+v", e)
        }
    case <-time.After(time.Second):
        t.Fatal("handler not called within 1s")
    }
}
```

Note: Read `nats_test.go` to understand how `fakeNatsClient` and its `deliver`
method work. Use the exact same pattern. If `deliver` has a different name, use
the correct name from the file.

**Verification commands**
```
go build ./...
go test ./event/...
go test ./...
go vet ./...
```

---

## PHASE 2 — DEAD CODE REMOVAL

---

### Task 2.1 — Remove unused dead code from `binders/cache.go` [x]

**Problem**
Three items in `binders/cache.go` are parsed and stored but never consumed:
- `rules []rule` field in `fieldMeta` struct
- `rule` struct
- `parseRules(tag string) []rule` function
- `buildFieldIndex(fields []fieldMeta)` function

The `bind()` function in `binders/json.go` never iterates `meta.rules` and
never calls `buildFieldIndex`. Validation happens in the middleware's separate
`validation.Validate()` step, not in the binder.

**Files to read first**
1. `go-core/binders/cache.go` — read the ENTIRE file
2. `go-core/binders/json.go` — read the `bind()` function to confirm rules are
   never used (search for any reference to `.rules` or `buildFieldIndex`)

**Before deleting, run these grep commands and confirm zero results outside cache.go:**
```
grep -r "parseRules" go-core/ --include="*.go"
grep -r "buildFieldIndex" go-core/ --include="*.go"
grep -r "\.rules" go-core/binders/ --include="*.go"
grep -r "rule{" go-core/binders/ --include="*.go"
```
Only proceed if all four greps return zero matches outside `cache.go` itself.

**Exact steps**
1. Open `go-core/binders/cache.go`.
2. Delete the `rule` struct entirely (the block starting with `type rule struct`).
3. Delete the `rules []rule` field from the `fieldMeta` struct.
4. Delete the `parseRules(tag string) []rule` function entirely.
5. Delete the `buildFieldIndex(fields []fieldMeta) map[string]*fieldMeta`
   function entirely.
6. In `buildFieldMeta`, remove these two lines:
   ```
   validationTag := sf.Tag.Get("validation")
   rules := parseRules(validationTag)
   ```
   And remove `rules: rules,` from the `fieldMeta{...}` struct literal.
7. After all deletions, verify that the `"strings"` import is still needed
   (it is, for `strings.Cut` in `parseRules`... but wait, `parseRules` is being
   deleted). After deleting `parseRules`, check whether `"strings"` is still
   used elsewhere in `cache.go`. If not, remove it from the import block.
8. Run `go build ./...` — it must compile cleanly.

**Test to write**
No new test is needed. The existing `binders` tests confirm coercion still works
after removing dead code. Run:
```
go test ./binders/...
```
All existing tests must pass.

**Verification commands**
```
go build ./...
go test ./binders/...
go test ./...
go vet ./...
```

---

### Task 2.2 — Delete `internal/config/` package [x]

**Problem**
`internal/config/config.go` contains its own `Config` struct with unexported
fields and its own `LoadConfig` function. It duplicates `bootstrap/config.go`
and `bootstrap/config_loader.go`. No framework package imports it.

**Before deleting, run grep and confirm zero callers:**
```
grep -r "go-core/internal/config" go-core/ --include="*.go"
```
This must return zero results. Only then proceed.

**Files to read first**
1. `go-core/internal/config/config.go` — confirm its contents and that it is
   a standalone package with no outward callers

**Exact steps**
1. Run the grep command above. If it returns any results, stop and report them.
2. If zero results: delete the directory `go-core/internal/config/` and all
   files inside it (`config.go`, `config_test.go`, `validate_test.go`, or
   whatever files exist inside that directory).
3. Run `go build ./...` immediately.

**Test to write**
No new test needed. Run the full suite to confirm nothing broke:
```
go test ./...
```

**Verification commands**
```
go build ./...
go test ./...
go vet ./...
```

---

### Task 2.3 — Delete `worker/worker.go_example` [x]

**Problem**
The file `worker/worker.go_example` is not compiled by the Go toolchain (the
`.go_example` extension prevents it). It describes a job worker pattern but
serves no structural purpose in the framework. It belongs in documentation or
the example app, not in the production package.

**Before deleting, confirm the file exists:**
```
grep -r "worker.go_example" go-core/ --include="*.go"
```
This should return nothing (the file is not imported anywhere).

**Exact steps**
1. Delete the file `go-core/worker/worker.go_example`.
2. Run `go build ./...` immediately.

**Test to write**
No new test needed.

**Verification commands**
```
go build ./...
go test ./worker/...
go test ./...
go vet ./...
```

---

### Task 2.4 — Delete package-level auth globals [x]

**Problem**
Two files in the `auth` package contain mutable package-level globals:
- `auth/hasher.go`: `var defaultHasher Hasher`, `HashPassword()`,
  `CheckPasswordHash()` — bypass dependency injection and expose mutable state.
- `auth/rbac.go`: `var DefaultAuthorizer Authorizer` — a mutable global that
  controls security decisions and can be monkey-patched at runtime.

**Files to read first**
1. `go-core/auth/hasher.go` — read the ENTIRE file
2. `go-core/auth/rbac.go` — read the ENTIRE file
3. `go-core/auth/rbac_test.go` — read the ENTIRE file

**Before deleting, run grep for every symbol being removed:**
```
grep -r "HashPassword\b" go-core/ --include="*.go"
grep -r "CheckPasswordHash\b" go-core/ --include="*.go"
grep -r "defaultHasher\b" go-core/ --include="*.go"
grep -r "DefaultAuthorizer\b" go-core/ --include="*.go"
```
Update every call site found before deleting.

**Exact steps**

Part A — `auth/hasher.go`:
1. Open `go-core/auth/hasher.go`.
2. Delete the line: `var defaultHasher Hasher = &BcryptHasher{}`
3. Delete the entire `HashPassword(password string) (string, error)` function.
4. Delete the entire `CheckPasswordHash(password, hash string) bool` function.
5. Keep the `Hasher` interface, `BcryptHasher` struct, and its two methods
   (`Hash` and `Compare`) — do NOT delete them.

Part B — `auth/rbac.go`:
1. Open `go-core/auth/rbac.go`.
2. Delete the `var DefaultAuthorizer Authorizer = func(...) bool { ... }` block.
3. Change the `Authorize` function to embed the authorizer logic inline. The
   current `Authorize` calls `AuthorizedWith(policy, DefaultAuthorizer)`. After
   deletion of `DefaultAuthorizer`, replace `Authorize` with:
   ```go
   // Authorize returns a middleware that checks if the authenticated user has
   // the given policy. The user must implement either HasPolicy(string) bool
   // or GetPolicies() []string.
   func Authorize(policy Policy) gin.HandlerFunc {
       authorizer := func(user Identifiable, p Policy) bool {
           type hasPolicy interface {
               HasPolicy(string) bool
           }
           if hp, ok := user.(hasPolicy); ok {
               return hp.HasPolicy(p.String())
           }
           type getPolicies interface {
               GetPolicies() []string
           }
           if gp, ok := user.(getPolicies); ok {
               for _, pol := range gp.GetPolicies() {
                   if pol == p.String() {
                       return true
                   }
               }
           }
           return false
       }
       return AuthorizedWith(policy, authorizer)
   }
   ```
4. Read `auth/rbac_test.go`. If any test references `DefaultAuthorizer` directly
   (e.g., `auth.DefaultAuthorizer(user, policy)`), update those tests to call
   `Authorize(policy)` and test via the middleware instead, or create an
   equivalent local authorizer function inside the test.

**Test to write**
No new test needed. Existing `auth/rbac_test.go` tests must pass.
If you had to update any test, include the change in this task.

**Verification commands**
```
go build ./...
go test ./auth/...
go test ./...
go vet ./...
```

---

### Task 2.5 — Delete `database.Migrate` wrapper function [x]

**Problem**
`database.Migrate` is a one-line alias for `database.SafeMigrate`. The name
implies a difference (safe vs. unsafe) but they are identical. The alias
misleads callers and duplicates the API surface.

**Files to read first**
1. `go-core/database/migrator.go` — read the ENTIRE file

**Before deleting, run grep to find all callers of Migrate (not SafeMigrate):**
```
grep -rn "\.Migrate(" go-core/ --include="*.go"
grep -rn "database\.Migrate(" go-core/ --include="*.go"
```
Note: the grep will also match `SafeMigrate(` — look only for bare `Migrate(`
without the `Safe` prefix. Update those callers to `SafeMigrate`.

**Exact steps**
1. Run the grep commands above.
2. For every call site that uses `database.Migrate(` (without `Safe`), change
   it to `database.SafeMigrate(`.
3. Open `go-core/database/migrator.go`.
4. Delete the entire `Migrate` function:
   ```
   func Migrate(db *gorm.DB, models ...interface{}) error {
       return SafeMigrate(db, models...)
   }
   ```
5. Do NOT change `SafeMigrate` or anything else in the file.

**Test to write**
No new test needed. Run existing migrator tests:
```
go test ./database/...
```

**Verification commands**
```
go build ./...
go test ./database/...
go test ./...
go vet ./...
```

---

## PHASE 3 — DUPLICATE LOGIC ELIMINATION

---

### Task 3.1 — Merge `database/testutil.go` + `database/testing.go` into one file [x]

**Problem**
Two separate files in the `database` package both provide test helpers:
- `testutil.go`: `PrepareTestDB`, `MustPrepareTestDB`
- `testing.go`: `NewTestRegistry`, `SimulateConnectionLoss`, `openSQLiteMemory`
Both serve identical audiences and belong in one file.

**Files to read first**
1. `go-core/database/testutil.go` — read the ENTIRE file
2. `go-core/database/testing.go` — read the ENTIRE file
3. `go-core/database/testing_test.go` — read the ENTIRE file (if it exists)

**Exact steps**
1. Create a new file `go-core/database/test_helpers.go`.
2. Set the package declaration to `package database`.
3. Merge the import blocks from both source files. Deduplicate — do not list
   any import twice.
4. Copy the full content of `testing.go` into `test_helpers.go` (excluding the
   `package database` line and import block, which you already wrote).
5. Copy the full content of `testutil.go` into `test_helpers.go` (excluding the
   `package database` line and import block).
6. Run `go build ./...`. Fix any errors (likely duplicate function definitions
   if any function appears in both files — check carefully).
7. Delete `go-core/database/testutil.go`.
8. Delete `go-core/database/testing.go`.
9. If `go-core/database/testing_test.go` exists, rename it to
   `go-core/database/test_helpers_test.go`. Use the move_path tool for this.
10. Run `go build ./...` again.

**Test to write**
No new test needed. Existing tests confirm the functions work:
```
go test ./database/...
```

**Verification commands**
```
go build ./...
go test ./database/...
go test ./...
go vet ./...
```

---

### Task 3.2 — Extract shared `resolveFromClaims` helper for JWT and OIDC providers [x]

**Problem**
`JWTProvider.Verify` and `OIDCProvider.Verify` both contain identical post-parse
logic: check `token.Valid`, optionally validate `claims.Issuer`, then call
`resolver(ctx, claims.Subject)`. This three-step sequence is duplicated verbatim.

**Files to read first**
1. `go-core/auth/provider.go` — read the ENTIRE file
2. `go-core/auth/oidc_provider.go` — read the ENTIRE file, especially `Verify`
3. `go-core/auth/jwt.go` — read lines 1–20 to confirm the Claims struct name
   and the jwt package import path

**Exact steps**
1. Open `go-core/auth/provider.go`.
2. At the end of the file, add a new unexported helper function:
   ```go
   // resolveFromClaims validates a successfully-parsed JWT token and resolves
   // the caller identity. It checks token.Valid, optionally validates issuer,
   // then delegates to the resolver.
   // issuer may be empty string (skips issuer check).
   func resolveFromClaims(
       ctx context.Context,
       token *jwt.Token,
       claims *Claims,
       issuer string,
       resolver IdentityResolver,
   ) (Identifiable, error) {
       if !token.Valid {
           return nil, ErrInvalidClaims
       }
       if issuer != "" && claims.Issuer != issuer {
           return nil, ErrInvalidClaims
       }
       return resolver(ctx, claims.Subject)
   }
   ```
   Note: `jwt.Token` is from the package `github.com/golang-jwt/jwt/v5`. Verify
   this import is already present in `provider.go` by reading its import block.
   If it is not present, add it. Read the import block before writing.
3. In `JWTProvider.Verify`, find the three lines that check `token.Valid`,
   check `claims.Issuer`, and call `p.resolver`. Read the exact lines from the
   file. Replace those three lines with:
   ```go
   return resolveFromClaims(ctx, token, claims, p.cfg.Issuer, p.resolver)
   ```
4. Open `go-core/auth/oidc_provider.go`.
5. In `OIDCProvider.Verify`, find the analogous three lines. Replace them with:
   ```go
   return resolveFromClaims(ctx, token, claims, p.issuer, p.resolver)
   ```

**Test to write**
No new test needed. Run existing auth tests — they already cover Verify:
```
go test ./auth/...
```

**Verification commands**
```
go build ./...
go test ./auth/...
go test ./...
go vet ./...
```

---

### Task 3.3 — Eliminate apply-to-both duplication in `datatable.Get()` [x]

**Problem**
In `datatable/datatable.go`'s `Get()` method, every search word, filter, and
view applies identical operations to both `query` and `countQuery`, duplicating
the same two-line pattern over and over:
```
query = something(query)
countQuery = something(countQuery)
```

**Files to read first**
1. `go-core/datatable/datatable.go` — read the `Get()` method completely
   (use start_line/end_line if the file is large)

**Exact steps**
1. Open `go-core/datatable/datatable.go`.
2. In the `Get()` method, after the lines:
   ```
   query := d.db.Session(&gorm.Session{})
   countQuery := d.db.Session(&gorm.Session{})
   ```
   Add a local helper closure:
   ```go
   // applyBoth applies fn to both the data query and the count query.
   applyBoth := func(fn func(*gorm.DB) *gorm.DB) {
       query = fn(query)
       countQuery = fn(countQuery)
   }
   ```
3. In the search section, find every block of the form:
   ```
   query = query.Where(sql, values...)
   countQuery = countQuery.Where(sql, values...)
   ```
   Replace each with:
   ```go
   captured := values // capture to avoid loop variable issue
   capturedSQL := sql
   applyBoth(func(q *gorm.DB) *gorm.DB { return q.Where(capturedSQL, captured...) })
   ```
   Note: inside the for-loop over words, you must capture `sql` and `values`
   as local variables before passing to the closure, to avoid the classic Go
   loop-variable capture bug.

4. In the filters section, find:
   ```
   query = f.Query(query, val, d.tableName)
   countQuery = f.Query(countQuery, val, d.tableName)
   ```
   Replace with:
   ```go
   capturedF := f
   capturedVal := val
   applyBoth(func(q *gorm.DB) *gorm.DB { return capturedF.Query(q, capturedVal, d.tableName) })
   ```
5. In the views section, find:
   ```
   query = view.Query(query, d.tableName)
   countQuery = view.Query(countQuery, d.tableName)
   ```
   Replace with:
   ```go
   capturedView := view
   applyBoth(func(q *gorm.DB) *gorm.DB { return capturedView.Query(q, d.tableName) })
   ```
6. Do NOT change anything in `Get()` beyond the three substitution sites above.
   The count query, pagination query, and return statement are unchanged.

**Test to write**
No new test needed. Existing datatable tests (if any) must pass. If there are
no existing tests for `Get()`, run at minimum:
```
go test ./datatable/...
```

**Verification commands**
```
go build ./...
go test ./datatable/...
go test ./...
go vet ./...
```

---

## PHASE 4 — PERFORMANCE IMPROVEMENTS

---

### Task 4.1 — Run `WithCount` queries in parallel in `resource/resource.go` [x]

**Problem**
`FindByID` runs N sequential `SELECT COUNT(*)` queries for N `WithCount` entries.
Each query is a separate DB roundtrip. With N=3, this means 3 serial roundtrips
that could run simultaneously.

**Files to read first**
1. `go-core/resource/resource.go` — read the ENTIRE file, focus on `FindByID`
2. `go-core/go.mod` — confirm `golang.org/x/sync` is a direct dependency

**Exact steps**
1. Open `go-core/resource/resource.go`.
2. Add `"golang.org/x/sync/errgroup"` to the import block. Also add `"context"`
   if it is not already imported. Read the import block before adding.
3. Define a local helper struct inside `FindByID` to hold each count result:
   ```go
   type countResult struct {
       key   string
       total int64
   }
   ```
   Place this INSIDE the `FindByID` function body, after the author-loading
   section and before the counts loop.
4. Replace the sequential counts for loop:
   ```
   for _, count := range r.counts {
       var total int64
       countQuery := r.db.Session(...) ...
       err := countQuery.Count(&total).Error
       ...
       response.Meta[countKey] = total
   }
   ```
   With this parallel version:
   ```go
   results := make([]countResult, len(r.counts))
   g, gCtx := errgroup.WithContext(context.Background())
   for i, count := range r.counts {
       i, count := i, count // capture loop variables for goroutine
       g.Go(func() error {
           var total int64
           cq := r.db.Session(&gorm.Session{NewDB: true}).
               WithContext(gCtx).
               Table(count.table).
               Select("COUNT(*)").
               Where(fmt.Sprintf("%s = ?", database.QuoteColumn(count.foreignKey)), id)
           if count.clause != "" {
               cq = cq.Where(count.clause)
           }
           if err := cq.Count(&total).Error; err != nil {
               return err
           }
           results[i] = countResult{key: count.table + "_count", total: total}
           return nil
       })
   }
   if err := g.Wait(); err != nil {
       return response, err
   }
   for _, cr := range results {
       if cr.key != "" {
           response.Meta[cr.key] = cr.total
       }
   }
   ```
5. Verify `database.QuoteColumn` exists by reading `database/helpers.go` line 1–20
   to confirm the function name. It was added in the previous plan.
6. Verify `"fmt"` and `"github.com/wssto2/go-core/database"` are already in
   the import block. Read the imports before writing.

**Test to write**
File: `go-core/resource/resource_test.go` (read first if it exists, create if not).
Package: `package resource`

```go
func TestResource_FindByID_MultipleCountsReturnedInMeta(t *testing.T) {
    db, cleanup := database.MustPrepareTestDB()
    defer cleanup()

    // This is a compile-time and logic test only — we verify that
    // calling WithCount multiple times and FindByID returns all counts.
    // The exact implementation depends on your GORM models.
    // At minimum, build the Resource and assert no panic/error from
    // the parallel goroutine machinery:
    type Item struct {
        ID uint `gorm:"primaryKey"`
    }
    if err := db.AutoMigrate(&Item{}); err != nil {
        t.Fatalf("AutoMigrate: %v", err)
    }
    db.Create(&Item{ID: 1})

    _ = New[Item](db).
        WithCount("items", "id", "").
        WithCount("items", "id", "id > 0")
    // FindByID(1) will run 2 parallel COUNTs.
    // We cannot fully assert SQL behavior without a test fixture,
    // but we can assert no panic and the API compiles.
}
```

Also run with the race detector:
```
go test -race ./resource/...
```

**Verification commands**
```
go build ./...
go test -race ./resource/...
go test ./...
go vet ./...
```

---

### Task 4.2 — Add `QuoteColumn` to ORDER BY in `datatable/datatable.go` [x]

**Problem**
The ORDER BY clause in `Get()` uses the column name without backtick quoting:
```
query = query.Order(fmt.Sprintf("%s.%s %s", d.tableName, d.queryParams.OrderCol, safeDirection))
```
This is inconsistent with the rest of the codebase, which uses `QuoteColumn`
for all column references. Although the whitelist prevents SQL injection, the
inconsistency is a DX footgun for future developers.

**Files to read first**
1. `go-core/datatable/datatable.go` — read the `Get()` method, ORDER BY section
2. `go-core/database/helpers.go` — read lines 1–30 to confirm `QuoteColumn`
   signature: `func QuoteColumn(s string) string`

**Exact steps**
1. Open `go-core/datatable/datatable.go`.
2. Find the ORDER BY line in `Get()`:
   ```
   query = query.Order(fmt.Sprintf("%s.%s %s", d.tableName, d.queryParams.OrderCol, safeDirection))
   ```
3. Replace it with:
   ```go
   query = query.Order(fmt.Sprintf("%s.%s %s", d.tableName, database.QuoteColumn(d.queryParams.OrderCol), safeDirection))
   ```
4. Verify that `"github.com/wssto2/go-core/database"` is already in the import
   block of `datatable.go`. Read the import block before writing.
5. Do NOT change anything else.

**Test to write**
No new test needed. Existing datatable tests must pass:
```
go test ./datatable/...
```

**Verification commands**
```
go build ./...
go test ./datatable/...
go test ./...
go vet ./...
```

---

### Task 4.3 — Replace fragile `isPanic` string check with typed error in `worker/manager.go` [x]

**Problem**
`isPanic` detects panics by checking if the error message starts with the
string `"panic: "`. This is fragile: the detection breaks if the prefix changes,
and it uses a manual length check (`len(err.Error()) > 7`) instead of
`strings.HasPrefix`. A typed error is the correct Go idiom.

**Files to read first**
1. `go-core/worker/manager.go` — read the ENTIRE file

**Exact steps**
1. Open `go-core/worker/manager.go`.
2. Add `"errors"` to the import block if it is not already there. Read the
   import block to check before adding.
3. Add a new unexported error type ABOVE the `Manager` struct definition:
   ```go
   // panicError wraps a recovered panic value for type-safe identification.
   // It is returned by safeRun when a worker goroutine panics.
   type panicError struct {
       value any
   }

   func (e *panicError) Error() string {
       return fmt.Sprintf("panic: %v", e.value)
   }
   ```
4. In `safeRun`, find:
   ```
   if r := recover(); r != nil {
       err = fmt.Errorf("panic: %v", r)
   }
   ```
   Replace with:
   ```go
   if r := recover(); r != nil {
       err = &panicError{value: r}
   }
   ```
5. In `runWorker`, find the block that calls `isPanic(err)`:
   ```
   if isPanic(err) {
       m.metrics.workerPanics.WithLabelValues(w.Name()).Inc()
   } else {
       m.metrics.workerErrors.WithLabelValues(w.Name()).Inc()
   }
   ```
   Replace with:
   ```go
   var pe *panicError
   if errors.As(err, &pe) {
       m.metrics.workerPanics.WithLabelValues(w.Name()).Inc()
   } else {
       m.metrics.workerErrors.WithLabelValues(w.Name()).Inc()
   }
   ```
6. Delete the `isPanic(err error) bool` function at the bottom of the file.
7. Verify `"fmt"` is still in the import block (it is used in `panicError.Error()`
   and elsewhere). Do not remove it.

**Test to write**
Existing `worker/manager_test.go` tests already cover panic recovery.
Run them to confirm the typed error works:
```
go test ./worker/...
```
No new test function is required, but if the existing test asserts on the error
message string `"panic: ..."`, it still works since `panicError.Error()` returns
the same string format.

**Verification commands**
```
go build ./...
go test -race ./worker/...
go test ./...
go vet ./...
```

---

## PHASE 5 — DI CONTAINER PROMOTION

---

### Task 5.1 — Replace `bootstrap.Container` with a cycle-detecting DI container [x]

**Problem**
Two DI containers exist in the codebase:
- `bootstrap.Container`: simple map-based, no cycle detection, value-binding only
- `internal/di.Container`: provider-function based, DFS cycle detection, lazy init

The `internal/di.Container` has zero callers outside its own tests. The goal is
to merge both: keep the `Bind[S]`/`Resolve[S]` API of bootstrap.Container but
add Register() provider functions and Build() cycle detection from internal/di.

**Files to read first (read ALL of them before writing one line)**
1. `go-core/bootstrap/container.go` — read the ENTIRE file
2. `go-core/internal/di/di.go` — read the ENTIRE file
3. `go-core/bootstrap/builder.go` — read the ENTIRE file (every Bind call)
4. `go-core/bootstrap/app.go` — read the ENTIRE file
5. `go-core/bootstrap/event.go` — read the ENTIRE file
6. `go-core/bootstrap/container_test.go` — read the ENTIRE file
7. `go-core/bootstrap/transactor_test.go` — read the ENTIRE file (if it exists)

Run this grep to find EVERY call to Bind, Rebind, Resolve, MustResolve in bootstrap:
```
grep -rn "Bind\[" go-core/bootstrap/ --include="*.go"
grep -rn "Resolve\[" go-core/bootstrap/ --include="*.go"
grep -rn "MustResolve\[" go-core/bootstrap/ --include="*.go"
```

**Architecture of the new Container (read ARCHITECTURE.md section 5 carefully)**
The new Container has TWO storage maps:
- `direct map[reflect.Type]any` — values from `Bind[S]` (always wins in resolution)
- `providers map[reflect.Type]*providerInfo` — functions from `Register()`

`resolveByType` checks `direct` first, then a lazy-init cache `instances`, then
calls the provider function if found in `providers`.

**Exact steps**

**Step 1: Rewrite `bootstrap/container.go` completely.**
Delete all existing content and write the new implementation:

```go
package bootstrap

import (
    "fmt"
    "reflect"
    "strings"
    "sync"

    "github.com/wssto2/go-core/database"
)

// providerInfo holds a registered provider function and its declared deps.
type providerInfo struct {
    fn   reflect.Value  // the provider function as a reflect.Value
    out  reflect.Type   // return type T (the key in the providers map)
    deps []reflect.Type // declared input types (dependencies)
}

// Container is a type-safe service container that supports both direct value
// binding (Bind) and provider-function registration (Register).
// Call Build() after all registrations to validate the dependency graph.
type Container struct {
    mu        sync.RWMutex
    direct    map[reflect.Type]any           // values from Bind[S]
    providers map[reflect.Type]*providerInfo  // functions from Register()
    instances map[reflect.Type]any            // lazy singleton cache for providers
    strict    bool
}

// NewContainer creates an empty Container.
func NewContainer() *Container {
    return &Container{
        direct:    make(map[reflect.Type]any),
        providers: make(map[reflect.Type]*providerInfo),
        instances: make(map[reflect.Type]any),
    }
}

// EnableStrictMode configures the container to panic on duplicate Bind calls
// and on Resolve calls for unregistered types.
func (c *Container) EnableStrictMode() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.strict = true
}

// Bind registers a concrete value directly. No dependencies are declared.
// In strict mode, panics if the type is already registered.
// Use Rebind for intentional overwrites (e.g., swapping InMemoryBus for NATSBus).
func Bind[S any](c *Container, val S) {
    if c == nil {
        panic("bootstrap: Bind called on nil container")
    }
    typ := reflect.TypeFor[S]()
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.strict {
        if _, exists := c.direct[typ]; exists {
            panic(fmt.Sprintf("bootstrap: duplicate Bind for %v", typ))
        }
    }
    c.direct[typ] = val
}

// Rebind overwrites a previously registered type without panicking in strict mode.
// Use this when intentionally replacing a service binding.
func Rebind[S any](c *Container, val S) {
    if c == nil {
        panic("bootstrap: Rebind called on nil container")
    }
    typ := reflect.TypeFor[S]()
    c.mu.Lock()
    defer c.mu.Unlock()
    c.direct[typ] = val
}

// Register accepts a provider function with signature: func(...deps) (T, error).
// Dependencies are declared implicitly by the function's parameter types.
// Providers are resolved lazily on first Resolve call.
// Call Build() after all Register calls to validate for cycles.
func (c *Container) Register(providerFn any) error {
    if c == nil {
        return fmt.Errorf("bootstrap: Register called on nil container")
    }
    v := reflect.ValueOf(providerFn)
    t := v.Type()
    if t.Kind() != reflect.Func {
        return fmt.Errorf("bootstrap: Register: provider must be a function, got %T", providerFn)
    }
    if t.NumOut() != 2 {
        return fmt.Errorf("bootstrap: Register: provider must return (T, error), got %d outputs", t.NumOut())
    }
    errorType := reflect.TypeOf((*error)(nil)).Elem()
    if !t.Out(1).Implements(errorType) {
        return fmt.Errorf("bootstrap: Register: second return value must implement error")
    }
    retType := t.Out(0)
    deps := make([]reflect.Type, t.NumIn())
    for i := 0; i < t.NumIn(); i++ {
        deps[i] = t.In(i)
    }
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.strict {
        if _, exists := c.providers[retType]; exists {
            panic(fmt.Sprintf("bootstrap: duplicate Register for %v", retType))
        }
    }
    c.providers[retType] = &providerInfo{fn: v, out: retType, deps: deps}
    return nil
}

// Build validates the provider dependency graph for circular dependencies using
// depth-first search. Only provider-function registrations (Register) can form
// cycles. Direct value bindings (Bind) have no declared dependencies.
// Call Build() once after all Register calls, before any Resolve call.
func (c *Container) Build() error {
    c.mu.RLock()
    graph := make(map[reflect.Type][]reflect.Type, len(c.providers))
    for out, prov := range c.providers {
        for _, d := range prov.deps {
            if _, ok := c.providers[d]; ok {
                graph[out] = append(graph[out], d)
            }
        }
    }
    c.mu.RUnlock()

    state := make(map[reflect.Type]int) // 0=unseen 1=visiting 2=done
    var stack []reflect.Type

    var visit func(reflect.Type) error
    visit = func(n reflect.Type) error {
        if state[n] == 1 {
            // cycle detected — reconstruct the path
            idx := -1
            for i := len(stack) - 1; i >= 0; i-- {
                if stack[i] == n {
                    idx = i
                    break
                }
            }
            var path []string
            if idx >= 0 {
                for i := idx; i < len(stack); i++ {
                    path = append(path, stack[i].String())
                }
                path = append(path, n.String())
            } else {
                path = []string{n.String()}
            }
            return fmt.Errorf("bootstrap: circular dependency detected: %s",
                strings.Join(path, " -> "))
        }
        if state[n] == 2 {
            return nil
        }
        state[n] = 1
        stack = append(stack, n)
        for _, nei := range graph[n] {
            if err := visit(nei); err != nil {
                return err
            }
        }
        stack = stack[:len(stack)-1]
        state[n] = 2
        return nil
    }

    for node := range graph {
        if state[node] == 0 {
            if err := visit(node); err != nil {
                return err
            }
        }
    }
    return nil
}

// resolveByType returns a singleton instance for the given type.
// Checks direct bindings first, then the lazy-init provider cache.
func (c *Container) resolveByType(typ reflect.Type) (any, error) {
    // Check direct bindings (from Bind) — these are always preferred.
    c.mu.RLock()
    if val, ok := c.direct[typ]; ok {
        c.mu.RUnlock()
        return val, nil
    }
    // Check lazy singleton cache (from previous provider resolutions).
    if inst, ok := c.instances[typ]; ok {
        c.mu.RUnlock()
        return inst, nil
    }
    prov, ok := c.providers[typ]
    c.mu.RUnlock()

    if !ok {
        if c.strict {
            panic(fmt.Sprintf("bootstrap: service not found: %v", typ))
        }
        return nil, fmt.Errorf("bootstrap: service not found: %v", typ)
    }

    // Resolve each dependency recursively.
    args := make([]reflect.Value, len(prov.deps))
    for i, depType := range prov.deps {
        dep, err := c.resolveByType(depType)
        if err != nil {
            return nil, fmt.Errorf("bootstrap: resolving dependency %v for %v: %w",
                depType, typ, err)
        }
        args[i] = reflect.ValueOf(dep)
    }

    // Call the provider function.
    outs := prov.fn.Call(args)
    if !outs[1].IsNil() {
        return nil, outs[1].Interface().(error)
    }
    inst := outs[0].Interface()

    // Store in lazy singleton cache (double-checked locking).
    c.mu.Lock()
    if existing, ok := c.instances[typ]; ok {
        c.mu.Unlock()
        return existing, nil // another goroutine beat us here
    }
    c.instances[typ] = inst
    c.mu.Unlock()
    return inst, nil
}

// Resolve retrieves a service by type. Returns an error if not found.
func Resolve[S any](c *Container) (S, error) {
    var zero S
    if c == nil {
        return zero, fmt.Errorf("bootstrap: Resolve called on nil container")
    }
    typ := reflect.TypeFor[S]()
    v, err := c.resolveByType(typ)
    if err != nil {
        return zero, err
    }
    s, ok := v.(S)
    if !ok {
        return zero, fmt.Errorf("bootstrap: type assertion failed: stored %T, requested %v", v, typ)
    }
    return s, nil
}

// MustResolve retrieves a service by type or panics if not found.
func MustResolve[S any](c *Container) S {
    val, err := Resolve[S](c)
    if err != nil {
        panic(err)
    }
    return val
}

// ResolveTransactor returns a database.Transactor for the named connection.
// Pass an empty string to use the primary connection.
func ResolveTransactor(c *Container, dbName string) (database.Transactor, error) {
    reg, err := Resolve[*database.Registry](c)
    if err != nil {
        return nil, err
    }
    if dbName == "" {
        dbName = reg.PrimaryName()
    }
    conn, err := reg.Get(dbName)
    if err != nil {
        return nil, fmt.Errorf("bootstrap: database connection %q not found", dbName)
    }
    return database.NewTransactor(conn), nil
}
```

**Step 2: Update `bootstrap/builder.go`.**
Open `bootstrap/builder.go`. Find every call that intentionally overwrites a
previously-registered type. Based on reading the file, these are:
- `WithNATSBus`: calls `Bind[event.Bus](b.container, n)` to overwrite the
  default in-memory bus. Change to `Rebind[event.Bus](b.container, n)`.
- `WithJWTAuth`: calls `Bind[auth.Provider](...)`. If this can be called after
  the default is set, change to `Rebind[auth.Provider](...)`.
- `WithDBTokenAuth`: same pattern. Change to `Rebind[auth.Provider](...)`.

All OTHER `Bind` calls in builder.go stay as `Bind` — do not change them.

**Step 3: Delete `internal/di/`.**
Run:
```
grep -r "internal/di" go-core/ --include="*.go"
```
If the only results are inside `go-core/internal/di/` itself and
`go-core/internal/enforce/`, read `go-core/internal/enforce/phase2_test.go`
to understand what it does with `internal/di`. If the enforce test only imports
`internal/dicheck` (not `internal/di`), then `internal/di` has zero callers and
can be deleted.

Delete `go-core/internal/di/` and all its files.
Run `go build ./...` immediately.

**Test to write**
File: `go-core/bootstrap/container_test.go`
Read the existing tests first. Add two new functions at the end:

```go
func TestContainer_Build_DetectsCircularDependency(t *testing.T) {
    c := NewContainer()

    // Register A that depends on B, and B that depends on A.
    typeA := reflect.TypeOf((*struct{ A bool })(nil)).Elem()
    typeB := reflect.TypeOf((*struct{ B bool })(nil)).Elem()

    // We cannot call Register directly with anonymous struct types easily,
    // so we use named types defined for this test.
    // Instead, test via the Build() logic directly using real provider funcs:
    type SvcA struct{}
    type SvcB struct{}

    if err := c.Register(func(b *SvcB) (*SvcA, error) { return &SvcA{}, nil }); err != nil {
        t.Fatalf("Register A: %v", err)
    }
    if err := c.Register(func(a *SvcA) (*SvcB, error) { return &SvcB{}, nil }); err != nil {
        t.Fatalf("Register B: %v", err)
    }

    err := c.Build()
    if err == nil {
        t.Fatal("Build() should have returned an error for circular dependency")
    }
    if !strings.Contains(err.Error(), "circular") {
        t.Fatalf("expected 'circular' in error, got: %v", err)
    }
}

func TestContainer_Rebind_DoesNotPanicInStrictMode(t *testing.T) {
    c := NewContainer()
    c.EnableStrictMode()

    type MyService struct{ ID int }
    Bind(c, &MyService{ID: 1})

    // Rebind must NOT panic even in strict mode.
    defer func() {
        if r := recover(); r != nil {
            t.Fatalf("Rebind panicked in strict mode: %v", r)
        }
    }()
    Rebind(c, &MyService{ID: 2})

    svc := MustResolve[*MyService](c)
    if svc.ID != 2 {
        t.Fatalf("expected ID=2 after Rebind, got %d", svc.ID)
    }
}
```

Add `"strings"` and `"reflect"` to the test file imports if not already present.

**Verification commands**
```
go build ./...
go test -race ./bootstrap/...
go test ./...
go vet ./...
```

---

## PHASE 6 — NAMING AND DEVELOPER EXPERIENCE

---

### Task 6.1 — Rename `binders.BindJSON` to `BindRequest` [x]

**Problem**
`binders.BindJSON` handles both `application/json` AND `multipart/form-data`.
The name `BindJSON` is misleading — it implies JSON only.

**Files to read first**
1. `go-core/binders/json.go` — read the ENTIRE file
2. `go-core/middlewares/bind.go` — read the ENTIRE file

**Before renaming, find all callers:**
```
grep -rn "binders\.BindJSON\b" go-core/ --include="*.go"
grep -rn "BindJSON(" go-core/ --include="*.go"
```

**Exact steps**
1. Open `go-core/binders/json.go`.
2. Renamed `func BindJSON[T any](ctx *gin.Context, v *T) error` to
   `func BindRequest[T any](ctx *gin.Context, v *T) error`.
   Changed only the function name — the body and parameters stay identical.
3. Open `go-core/middlewares/bind.go`.
4. Find `binders.BindJSON(ctx, &request)` and change to
   `binders.BindRequest(ctx, &request)`.
5. For every other call site found by grep, updated `BindJSON` to `BindRequest`.

**Test to write**
No new test needed. Existing binders and middlewares tests must pass:
```
go test ./binders/...
go test ./middlewares/...
```

**Verification commands**
```
go build ./...
go test ./binders/...
go test ./middlewares/...
go test ./...
go vet ./...
```

---

### Task 6.2 — Rename `testhelpers.NewTestDB` to `NewTestRegistry` [x]

**Problem**
`testhelpers.NewTestDB` returns `*database.Registry`, not `*gorm.DB`. The name
strongly implies a single database connection.

**Files to read first**
1. `go-core/testhelpers/db.go` — read the ENTIRE file

**Before renaming, find all callers:**
```
grep -rn "testhelpers\.NewTestDB\b" go-core/ --include="*.go"
grep -rn "NewTestDB(" go-core/ --include="*.go"
```

**Exact steps**
1. Open `go-core/testhelpers/db.go`.
2. Rename `func NewTestDB(...)` to `func NewTestRegistry(...)`.
   Change only the function name. The signature and body stay identical.
3. For every call site found by grep, change `testhelpers.NewTestDB(` to
   `testhelpers.NewTestRegistry(`.

**Test to write**
No new test needed. Run all tests to verify callers compile:
```
go test ./...
```

**Verification commands**
```
go build ./...
go test ./...
go vet ./...
```

---

### Task 6.3 — Fix `validation.Validator.Register` to return `error`; add `MustRegister` [x]

**Problem**
`validation.Validator.Register` panics on duplicate rule registration instead of
returning an error. Panicking in a non-Must function violates the project's
panic-only-in-Must convention.

**Files to read first**
1. `go-core/validation/validator.go` — read the ENTIRE file

**Before changing, find all callers of Register:**
```
grep -rn "\.Register(" go-core/ --include="*.go"
```
Note: this grep will match many things. Filter for `validator.Register` or
`v.Register` patterns. Update all callers.

**Exact steps**
1. Open `go-core/validation/validator.go`.
2. Rename the current panicking `Register(name string, rule Rule)` to
   `MustRegister(name string, rule Rule)`. Its body stays the same (it panics).
   Update the doc comment to say "MustRegister panics if the rule name is
   already registered."
3. Add a new `Register(name string, rule Rule) error` method that returns an
   error instead of panicking:
   ```go
   // Register adds a rule to the validator. Returns an error if the rule name
   // is already registered. Use MustRegister if you prefer a panic.
   func (v *Validator) Register(name string, rule Rule) error {
       if _, exists := v.registry[name]; exists {
           return fmt.Errorf("validator: rule %q is already registered", name)
       }
       v.registry[name] = rule
       return nil
   }
   ```
4. Add `"fmt"` to the import block if it is not already present.
5. For every call site found by grep that calls `.Register(` and ignores the
   result (old panicking version), decide whether to:
   a. Change to `.MustRegister(` if panic behavior is desired, OR
   b. Change to `.Register(` and handle the error.
   Read each call site to decide. If it was previously ignoring the return
   (impossible — old Register returned nothing), change to `MustRegister`.

**Test to write**
File: `go-core/validation/validator_test.go` (read first if it exists).
Add at the end:

```go
func TestValidator_Register_ReturnErrorOnDuplicate(t *testing.T) {
    v := New()
    // "required" is already in the default registry.
    err := v.Register("required", RequiredRule)
    if err == nil {
        t.Fatal("expected error when registering duplicate rule, got nil")
    }
}

func TestValidator_MustRegister_PanicsOnDuplicate(t *testing.T) {
    v := New()
    defer func() {
        if r := recover(); r == nil {
            t.Fatal("MustRegister should have panicked on duplicate rule")
        }
    }()
    v.MustRegister("required", RequiredRule) // should panic
}
```

**Verification commands**
```
go build ./...
go test ./validation/...
go test ./...
go vet ./...
```

---

### Task 6.4 — Rename `datatable.Datatable.WithQuery` to `WithScope` [x]

**Problem**
`WithQuery` applies persistent GORM constraints (base scopes) that apply to all
requests. The name overlaps confusingly with `WithFilter` and `WithView`, which
are also query-related but conditional. `WithScope` better describes "a base
constraint that is always applied."

**Files to read first**
1. `go-core/datatable/datatable.go` — read the ENTIRE file

**Before renaming, find all callers:**
```
grep -rn "\.WithQuery(" go-core/ --include="*.go"
grep -rn "datatable\..*WithQuery\b" go-core/ --include="*.go"
```

**Exact steps**
1. Open `go-core/datatable/datatable.go`.
2. Rename `func (d *Datatable[T]) WithQuery(...)` to `func (d *Datatable[T]) WithScope(...)`.
   Change only the function name. The parameters and body stay identical.
3. For every call site found by grep, change `.WithQuery(` to `.WithScope(`.

**Test to write**
No new test needed. Existing tests must pass:
```
go test ./datatable/...
```

**Verification commands**
```
go build ./...
go test ./datatable/...
go test ./...
go vet ./...
```

---

### Task 6.5 — Add `PageMeta()` to `DatatableResult`; fix `web.Paginated` to use it [x]

**Problem**
`web.Paginated` manually re-extracts pagination fields from `DatatableResult`
into a `gin.H{}` map. If `DatatableResult` gains or renames a field, `Paginated`
diverges silently. A `PageMeta()` method on `DatatableResult` is the single
source of truth.

**Files to read first**
1. `go-core/datatable/result.go` — read the ENTIRE file
2. `go-core/web/response.go` — read the ENTIRE file

**Exact steps**
1. Open `go-core/datatable/result.go`.
2. Add a new `PageMeta()` method to `DatatableResult[T]`:
   ```go
   // PageMeta returns a map of pagination metadata fields suitable for use
   // as the Meta field in a web.Response.
   func (d *DatatableResult[T]) PageMeta() map[string]any {
       return map[string]any{
           "total":     d.Total,
           "page":      d.Page,
           "per_page":  d.PerPage,
           "last_page": d.LastPage,
           "from":      d.From,
           "to":        d.To,
       }
   }
   ```
3. Open `go-core/web/response.go`.
4. Find the `Paginated` function. The current implementation manually builds a
   `gin.H{}` with the same fields. Replace it with:
   ```go
   // Paginated sends a 200 OK response with data and pagination metadata.
   func Paginated[T any](ctx *gin.Context, result *datatable.DatatableResult[T]) {
       JSON(ctx, http.StatusOK, result.Data, result.PageMeta())
   }
   ```

**Test to write**
File: `go-core/web/response_test.go` (read first).
Add at the end:

```go
func TestPaginated_MetaMatchesDatatableResult(t *testing.T) {
    gin.SetMode(gin.TestMode)
    w := httptest.NewRecorder()
    ctx, _ := gin.CreateTestContext(w)

    result := &datatable.DatatableResult[string]{
        Data:     []string{"a", "b"},
        Total:    10,
        Page:     2,
        PerPage:  2,
        LastPage: 5,
        From:     3,
        To:       4,
    }
    Paginated(ctx, result)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }
    body := w.Body.String()
    // Verify all pagination fields appear in the response body.
    for _, want := range []string{`"total":10`, `"page":2`, `"per_page":2`,
        `"last_page":5`, `"from":3`, `"to":4`} {
        if !strings.Contains(body, want) {
            t.Errorf("response body missing %q; body: %s", want, body)
        }
    }
}
```

Add `"strings"` to the test file imports if not present.

**Verification commands**
```
go build ./...
go test ./web/...
go test ./datatable/...
go test ./...
go vet ./...
```

---

## PHASE 7 — LOGIC RELOCATION

---

### Task 7.1 — Add `database.NewTransactorFromRegistry`; update `bootstrap.ResolveTransactor` to delegate [x]

**Problem**
`bootstrap.ResolveTransactor` contains database-specific logic: it knows how to
look up a connection by name from a `*database.Registry` and construct a
`Transactor`. This knowledge belongs in the `database` package.

**Files to read first**
1. `go-core/bootstrap/container.go` — read `ResolveTransactor`
2. `go-core/database/transactor.go` — read the ENTIRE file
3. `go-core/go.mod` — first line, to confirm module path

**Exact steps**
1. Open `go-core/database/transactor.go`.
2. Add a new exported function at the end of the file:
   ```go
   // NewTransactorFromRegistry creates a Transactor for the named connection
   // in reg. Pass an empty string for dbName to use the primary connection.
   func NewTransactorFromRegistry(reg *Registry, dbName string) (Transactor, error) {
       if dbName == "" {
           dbName = reg.PrimaryName()
       }
       conn, err := reg.Get(dbName)
       if err != nil {
           return nil, fmt.Errorf("database: connection %q not found", dbName)
       }
       return NewTransactor(conn), nil
   }
   ```
   Verify that `"fmt"` is already imported in `transactor.go`. Read the import
   block before writing. `Registry` is in the same package (`database`), so no
   new import is needed for it.
3. Open `go-core/bootstrap/container.go`.
4. Find `ResolveTransactor`. Replace its body with a delegation to the new
   database function:
   ```go
   func ResolveTransactor(c *Container, dbName string) (database.Transactor, error) {
       reg, err := Resolve[*database.Registry](c)
       if err != nil {
           return nil, err
       }
       return database.NewTransactorFromRegistry(reg, dbName)
   }
   ```

**Test to write**
File: `go-core/database/transactor_tx_test.go` (read first).
Add at the end:

```go
func TestNewTransactorFromRegistry_UsesNamedConnection(t *testing.T) {
    reg, cleanup := NewTestRegistry("primary")
    defer cleanup()

    tx, err := NewTransactorFromRegistry(reg, "primary")
    if err != nil {
        t.Fatalf("NewTransactorFromRegistry: %v", err)
    }
    if tx == nil {
        t.Fatal("expected non-nil Transactor")
    }
}

func TestNewTransactorFromRegistry_EmptyNameUsesPrimary(t *testing.T) {
    reg, cleanup := NewTestRegistry("primary")
    defer cleanup()

    tx, err := NewTransactorFromRegistry(reg, "")
    if err != nil {
        t.Fatalf("NewTransactorFromRegistry with empty name: %v", err)
    }
    if tx == nil {
        t.Fatal("expected non-nil Transactor")
    }
}
```

Note: `NewTestRegistry` was created in Task 3.1. Read `database/test_helpers.go`
to confirm its exact signature before using it.

**Verification commands**
```
go build ./...
go test ./database/...
go test ./bootstrap/...
go test ./...
go vet ./...
```

---

### Task 7.2 — Move `bootstrap/health.go` to a new `health/` package [x]

**Problem**
`bootstrap/health.go` contains 110+ lines of health-check infrastructure
(`HealthChecker`, `HealthRegistry`, `DBHealthChecker`, `LivenessHandler`,
`ReadinessHandler`). This logic is independent of bootstrap wiring and belongs
in its own package so it can be imported or tested independently.

**Files to read first**
1. `go-core/bootstrap/health.go` — read the ENTIRE file
2. `go-core/bootstrap/builder.go` — read `setupHealth()`
3. `go-core/bootstrap/app.go` — read `Shutdown()` for `SetDraining` usage
4. `go-core/bootstrap/health_handlers_test.go` — read the ENTIRE file
5. `go-core/bootstrap/health_test.go` — read the ENTIRE file

**Before moving, find every reference to health types in bootstrap:**
```
grep -rn "HealthRegistry\|HealthChecker\|DBHealthChecker\|LivenessHandler\|ReadinessHandler\|SetDraining\|IsDraining" go-core/ --include="*.go"
```

**Exact steps**
1. Create directory `go-core/health/`.
2. Create `go-core/health/health.go` with `package health`.
   Copy ALL types, interfaces, structs, and methods from `bootstrap/health.go`
   into this file. Change the package declaration from `package bootstrap` to
   `package health`.
   Add any required imports (e.g., `gorm.io/gorm` for `DBHealthChecker`,
   `"context"` for the Checker interface, `"net/http"` for handlers,
   `"github.com/gin-gonic/gin"` for gin.HandlerFunc).
   Read `bootstrap/health.go` imports carefully and replicate them.
3. Create `go-core/health/health_test.go` with `package health`.
   Copy the relevant test helpers and test functions from
   `bootstrap/health_handlers_test.go` and `bootstrap/health_test.go` that
   test the health types directly. Update imports to use `health.` prefix where
   needed.
4. Open `go-core/bootstrap/builder.go`.
   Add import `"github.com/wssto2/go-core/health"`.
   In `setupHealth()`, change all references to use the `health.` package prefix:
   - `NewHealthRegistry()` → `health.NewHealthRegistry()`  (or whatever the
     constructor is named — read health.go to confirm)
   - `NewDBHealthChecker(...)` → `health.NewDBHealthChecker(...)`
   - `LivenessHandler()` → `health.LivenessHandler(...)`
   - `ReadinessHandler(hr)` → `health.ReadinessHandler(hr)`
   Also change the `Bind[*HealthRegistry](b.container, hr)` to
   `Bind[*health.HealthRegistry](b.container, hr)`.
5. Open `go-core/bootstrap/app.go`.
   Find where `SetDraining(true)` is called (inside `Shutdown`).
   The current code calls `Resolve[*HealthRegistry](a.container)`.
   Change to `Resolve[*health.HealthRegistry](a.container)`.
   Add import `"github.com/wssto2/go-core/health"` to app.go.
6. Delete `go-core/bootstrap/health.go` after confirming that `go build ./...`
   passes with the new health package imported everywhere.
7. Delete the health-specific test code from `bootstrap/health_handlers_test.go`
   and `bootstrap/health_test.go` that has been moved to `health/health_test.go`.
   Keep any bootstrap-integration tests that test the wiring (not the health
   types themselves).

**Test to write**
File: `go-core/health/health_test.go`
The tests from bootstrap's health test files should be replicated here.
At minimum add:

```go
func TestHealthRegistry_SetAndGetDraining(t *testing.T) {
    hr := NewHealthRegistry()
    if hr.IsDraining() {
        t.Fatal("new registry should not be draining")
    }
    hr.SetDraining(true)
    if !hr.IsDraining() {
        t.Fatal("expected draining=true after SetDraining(true)")
    }
}
```

Read `bootstrap/health_test.go` to port any existing tests.

**Verification commands**
```
go build ./...
go test ./health/...
go test ./bootstrap/...
go test ./...
go vet ./...
```

---

### Task 7.3 — Move upload functions from `web/helpers.go` to `web/upload/upload.go` [x]

**Problem**
`web/helpers.go` mixes URL-parsing helpers (`GetParamInt`, `GetQueryInt`,
`GetPathID`) with 150+ lines of file-upload logic (`UploadFile`, `DeleteFile`,
`UploadConfig`, `UploadedFile`, `sanitiseFilename`, `extFromMIME`). Upload logic
is large enough to be its own sub-package.

**Files to read first**
1. `go-core/web/helpers.go` — read the ENTIRE file
2. `go-core/go.mod` — first line to confirm module path

**Before moving, find all callers of the upload functions:**
```
grep -rn "web\.UploadFile\|web\.DeleteFile\|UploadConfig\|UploadedFile" go-core/ --include="*.go"
```

**Exact steps**
1. Create directory `go-core/web/upload/`.
2. Create `go-core/web/upload/upload.go` with `package upload`.
3. Move the following from `web/helpers.go` into `upload/upload.go`:
   - `type UploadConfig struct { ... }`
   - `type UploadedFile struct { ... }`
   - `func sanitiseFilename(raw string) string`
   - `func extFromMIME(mimeType, fallbackFilename string) string`
   - `func UploadFile(ctx *gin.Context, formKey string, config UploadConfig) (UploadedFile, error)`
   - `func DeleteFile(basePath, relativePath string) error`
4. In `upload.go`, add all required imports. Read `web/helpers.go` import block
   and copy only those imports that are used by the moved functions. The package
   is `upload`, not `web`, so `gin` must be imported explicitly.
5. In `web/helpers.go`, delete all the moved code. Keep ONLY:
   - `func GetParamInt(ctx *gin.Context, key string) (int, bool)`
   - `func GetQueryInt(ctx *gin.Context, key string) (int, bool)`
   - `func GetPathID(ctx *gin.Context) (int, bool)`
   After deletion, remove any imports that are no longer used.
6. For every call site found by grep that uses `web.UploadFile` or
   `web.DeleteFile` or `web.UploadConfig` or `web.UploadedFile`, update the
   import to `"github.com/wssto2/go-core/web/upload"` and the function calls
   to `upload.UploadFile(...)`, `upload.DeleteFile(...)`, etc.
7. If `web/helpers_test.go` contains tests for upload functions (added in Task
   1.4), move those tests to `go-core/web/upload/upload_test.go` with
   `package upload`.

**Test to write**
File: `go-core/web/upload/upload_test.go` with `package upload`.
Port the upload test from Task 1.4 into this file. At minimum:

```go
func TestUploadFile_BaseDirEmpty_ReturnsBadRequest(t *testing.T) {
    gin.SetMode(gin.TestMode)
    w := httptest.NewRecorder()
    ctx, _ := gin.CreateTestContext(w)
    ctx.Request = httptest.NewRequest(http.MethodPost, "/", nil)

    _, err := UploadFile(ctx, "file", Config{BaseDir: ""})
    if err == nil {
        t.Fatal("expected error for empty BaseDir")
    }
    var appErr *apperr.AppError
    if !errors.As(err, &appErr) {
        t.Fatalf("expected *apperr.AppError, got %T", err)
    }
    if appErr.Code != apperr.CodeBadRequest {
        t.Fatalf("expected CodeBadRequest, got %s", appErr.Code)
    }
}
```

Note: In the `upload` package, `UploadConfig` is now named `Config` (since the
package is already named `upload`, the `Upload` prefix is redundant). Similarly,
`UploadedFile` becomes `File`. If you prefer to keep the original names to
minimize diff, that is also acceptable — read the file and be consistent.

**Verification commands**
```
go build ./...
go test ./web/...
go test ./web/upload/...
go test ./...
go vet ./...
```

---

### Task 7.4 — Parallel module registration in `bootstrap/app.go` [ ]

**Problem**
`registerModules()` calls `m.Register(container)` for each module sequentially.
Module `Register` calls are independent — they bind services into the container
and do not depend on other modules' registrations. They can safely run in
parallel using errgroup.

**Files to read first**
1. `go-core/bootstrap/app.go` — read `registerModules()` completely
2. `go-core/bootstrap/module.go` — read the `Module` interface
3. `go-core/go.mod` — confirm `golang.org/x/sync` is a direct dependency

**Exact steps**
1. Open `go-core/bootstrap/app.go`.
2. Verify the import `"golang.org/x/sync/errgroup"` is already present (it was
   added in a previous plan). If not, add it.
3. Replace the sequential `registerModules()` function:
   ```
   func (a *App) registerModules() error {
       for _, m := range a.modules {
           if err := m.Register(a.container); err != nil {
               return err
           }
       }
       return nil
   }
   ```
   With a parallel version:
   ```go
   func (a *App) registerModules() error {
       g, _ := errgroup.WithContext(context.Background())
       for _, m := range a.modules {
           m := m // capture loop variable
           g.Go(func() error {
               if err := m.Register(a.container); err != nil {
                   return fmt.Errorf("module %q register failed: %w", m.Name(), err)
               }
               return nil
           })
       }
       return g.Wait()
   }
   ```
4. Verify `"context"` and `"fmt"` are in the import block. Read the imports
   before writing.

**Test to write**
File: `go-core/bootstrap/app_test.go` (read first).
Add at the end:

```go
func TestRegisterModules_RunsInParallel(t *testing.T) {
    const numModules = 5
    started := make(chan struct{}, numModules)
    allStarted := make(chan struct{})

    type parallelModule struct {
        name string
    }

    modules := make([]Module, numModules)
    for i := 0; i < numModules; i++ {
        i := i
        modules[i] = &mockModule{
            name: fmt.Sprintf("mod-%d", i),
            registerFn: func(c *Container) error {
                started <- struct{}{}
                // Block until all modules have started, proving parallelism.
                select {
                case <-allStarted:
                case <-time.After(2 * time.Second):
                    return fmt.Errorf("timeout: modules did not start in parallel")
                }
                return nil
            },
        }
    }

    go func() {
        // Once all 5 have sent to started, signal allStarted.
        for i := 0; i < numModules; i++ {
            <-started
        }
        close(allStarted)
    }()

    app := NewApp(DefaultConfig(), NewContainer(), nil, nil, modules)
    if err := app.registerModules(); err != nil {
        t.Fatalf("registerModules: %v", err)
    }
}
```

Note: The `mockModule` type used here needs a `registerFn` field. Read the
existing `app_test.go` to see if `mockModule` already supports custom register
functions. If not, create a new type `funcModule` for this test only.

**Verification commands**
```
go build ./...
go test -race ./bootstrap/...
go test ./...
go vet ./...
```

---

## PHASE 8 — FEATURE ADDITIONS

---

### Task 8.1 — Inject `*slog.Logger` into `NATSBus`; log Subscribe errors [x]

**Problem**
`NATSBus.Subscribe` silently drops both unmarshal errors and handler errors.
These are invisible in production — failed event delivery leaves no trace.

**Files to read first**
1. `go-core/event/nats_adapter.go` — read the ENTIRE file
2. `go-core/event/nats_test.go` — read the ENTIRE file (to update call sites)
3. `go-core/bootstrap/builder.go` — search for `NewNATSBus` usage

**Before changing, find all callers of `NewNATSBus`:**
```
grep -rn "NewNATSBus(" go-core/ --include="*.go"
```

**Exact steps**
1. Open `go-core/event/nats_adapter.go`.
2. Add `"log/slog"` to the import block.
3. Add a `log *slog.Logger` field to the `NATSBus` struct:
   ```go
   type NATSBus struct {
       client NatsClient
       log    *slog.Logger
   }
   ```
4. Change `NewNATSBus` to accept a logger:
   ```go
   // NewNATSBus constructs a NATS-backed Bus. log is used to report subscribe
   // errors that cannot be returned to the caller (async delivery).
   func NewNATSBus(client NatsClient, log *slog.Logger) *NATSBus {
       if log == nil {
           log = slog.Default()
       }
       return &NATSBus{client: client, log: log}
   }
   ```
5. In `Subscribe`, find the two silent-drop sites:

   Site 1 — unmarshal error (when both Envelope and raw unmarshal fail):
   After both unmarshal attempts fail (when the code currently just `return`s),
   add logging BEFORE the return:
   ```go
   n.log.Error("nats: failed to unmarshal message",
       "subject", subject,
       "error", unmarshalErr)
   return
   ```
   Note: Read the exact control flow in Subscribe to place this correctly.
   The `return` that silently drops the unmarshal error is the one to log before.

   Site 2 — handler error:
   ```go
   _ = handler(ctx2, recv)
   ```
   Change to:
   ```go
   if err := handler(ctx2, recv); err != nil {
       // Handler errors are logged but not propagated — NATS delivery is async
       // and there is no caller to receive the error.
       n.log.Error("nats: handler returned error",
           "subject", subject,
           "error", err)
   }
   ```

6. Open `go-core/bootstrap/builder.go`.
   Find `event.NewNATSBus(client)` in `WithNATSBus`. Change to:
   ```go
   log := MustResolve[*slog.Logger](b.container)
   n := event.NewNATSBus(client, log)
   ```
   Read the actual code first to insert correctly without breaking other lines.

7. For every other caller of `NewNATSBus` found by grep, update the call to
   pass a `*slog.Logger` as the second argument.

**Test to write**
File: `go-core/event/nats_test.go` (read first).
The existing tests create `NATSBus` with `NewNATSBus(fake)`. Update every
existing `NewNATSBus(fake)` call to `NewNATSBus(fake, slog.Default())`.

Add a new test:
```go
func TestNATSBus_Subscribe_HandlerErrorIsLogged(t *testing.T) {
    // Use a custom slog handler to capture log output.
    var buf bytes.Buffer
    log := slog.New(slog.NewTextHandler(&buf, nil))

    fake := &fakeNatsClient{}
    bus := NewNATSBus(fake, log)

    err := bus.Subscribe(struct{ ID int }{}, func(ctx context.Context, ev any) error {
        return fmt.Errorf("intentional handler error")
    })
    if err != nil {
        t.Fatalf("Subscribe: %v", err)
    }

    data, _ := json.Marshal(struct{ ID int }{ID: 1})
    fake.deliver(data)

    // Give the async handler time to run.
    time.Sleep(50 * time.Millisecond)

    if !strings.Contains(buf.String(), "handler returned error") {
        t.Fatalf("expected error to be logged, got: %s", buf.String())
    }
}
```

Add `"bytes"`, `"strings"`, and `"fmt"` to the test file imports if not present.

**Verification commands**
```
go build ./...
go test ./event/...
go test ./bootstrap/...
go test ./...
go vet ./...
```

---

### Task 8.2 — Add `List` and `Exists` to `storage.Driver`; implement in `LocalDriver` [x]

**Problem**
The `storage.Driver` interface only supports `Put`, `Get`, `Delete`, `URL`.
Many storage use cases need `List` (enumerate keys by prefix) and `Exists`
(check key existence without reading). All implementations must provide these.

**Files to read first**
1. `go-core/storage/driver.go` — read the ENTIRE file
2. `go-core/storage/local/local.go` — read the ENTIRE file
3. `go-core/storage/local/local_test.go` — read the ENTIRE file

**Exact steps**
1. Open `go-core/storage/driver.go`.
2. Add two new methods to the `Driver` interface:
   ```go
   type Driver interface {
       Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error
       Get(ctx context.Context, key string) (io.ReadCloser, error)
       Delete(ctx context.Context, key string) error
       URL(ctx context.Context, key string) (string, error)
       // List returns all object keys that start with prefix.
       // An empty prefix returns all keys.
       List(ctx context.Context, prefix string) ([]string, error)
       // Exists reports whether an object with the given key exists.
       Exists(ctx context.Context, key string) (bool, error)
   }
   ```
3. Open `go-core/storage/local/local.go`.
4. Add the `List` implementation:
   ```go
   // List returns all file keys under Root that start with prefix.
   func (d *LocalDriver) List(ctx context.Context, prefix string) ([]string, error) {
       var keys []string
       err := filepath.WalkDir(d.Root, func(path string, entry fs.DirEntry, err error) error {
           if err != nil {
               return err
           }
           if entry.IsDir() {
               return nil
           }
           rel, relErr := filepath.Rel(d.Root, path)
           if relErr != nil {
               return relErr
           }
           rel = filepath.ToSlash(rel)
           if strings.HasPrefix(rel, prefix) {
               keys = append(keys, rel)
           }
           return nil
       })
       if err != nil {
           return nil, apperr.Internal(err)
       }
       return keys, nil
   }
   ```
   Add `"io/fs"` and `"strings"` to the import block if not already present.
   Read the existing import block before writing.

5. Add the `Exists` implementation:
   ```go
   // Exists reports whether a file with the given key exists in Root.
   func (d *LocalDriver) Exists(ctx context.Context, key string) (bool, error) {
       fullPath := filepath.Join(d.Root, key)
       _, err := os.Stat(fullPath)
       if err == nil {
           return true, nil
       }
       if os.IsNotExist(err) {
           return false, nil
       }
       return false, apperr.Internal(err)
   }
   ```

**Test to write**
File: `go-core/storage/local/local_test.go` (read first).
Append at the end:

```go
func TestLocalDriver_Exists(t *testing.T) {
    dir := t.TempDir()
    d, err := New(dir)
    if err != nil {
        t.Fatalf("New: %v", err)
    }
    ctx := context.Background()

    exists, err := d.Exists(ctx, "nonexistent.txt")
    if err != nil {
        t.Fatalf("Exists(nonexistent): %v", err)
    }
    if exists {
        t.Fatal("expected false for nonexistent key")
    }

    if err := d.Put(ctx, "hello.txt", strings.NewReader("hello"), 5, "text/plain"); err != nil {
        t.Fatalf("Put: %v", err)
    }
    exists, err = d.Exists(ctx, "hello.txt")
    if err != nil {
        t.Fatalf("Exists(hello.txt): %v", err)
    }
    if !exists {
        t.Fatal("expected true for existing key")
    }
}

func TestLocalDriver_List(t *testing.T) {
    dir := t.TempDir()
    d, err := New(dir)
    if err != nil {
        t.Fatalf("New: %v", err)
    }
    ctx := context.Background()

    d.Put(ctx, "a/foo.txt", strings.NewReader("1"), 1, "text/plain")
    d.Put(ctx, "a/bar.txt", strings.NewReader("2"), 1, "text/plain")
    d.Put(ctx, "b/baz.txt", strings.NewReader("3"), 1, "text/plain")

    keys, err := d.List(ctx, "a/")
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(keys) != 2 {
        t.Fatalf("expected 2 keys with prefix 'a/', got %d: %v", len(keys), keys)
    }

    all, err := d.List(ctx, "")
    if err != nil {
        t.Fatalf("List all: %v", err)
    }
    if len(all) != 3 {
        t.Fatalf("expected 3 total keys, got %d: %v", len(all), all)
    }
}
```

Add `"strings"` to the test file imports if not present.

**Verification commands**
```
go build ./...
go test ./storage/...
go test ./...
go vet ./...
```

---

### Task 8.3 — Add `OnError` callback to `AsyncRepository` for failed audit writes [x]

**Problem**
`AsyncRepository.loop()` ignores all errors from `underlying.Write()` with
`_ = a.underlying.Write(...)`. Failed audit writes are permanently lost with no
trace. An `OnError` callback gives callers the ability to log, metric, or DLQ
failed entries.

**Files to read first**
1. `go-core/audit/async.go` — read the ENTIRE file (including Task 1.1 changes)
2. `go-core/audit/repository.go` — read the ENTIRE file to confirm Entry type

**Exact steps**
1. Open `go-core/audit/async.go`.
2. Add an `OnError func(Entry, error)` field to the `AsyncRepository` struct.
   Place it after the `workers int` field and before the `closed atomic.Bool`
   field added in Task 1.1:
   ```go
   type AsyncRepository struct {
       underlying Repository
       ch         chan Entry
       wg         sync.WaitGroup
       closeOnce  sync.Once
       workers    int
       OnError    func(Entry, error) // called when underlying.Write fails; may be nil
       closed     atomic.Bool
   }
   ```
3. In `loop()`, find the line:
   ```
   _ = a.underlying.Write(context.Background(), e)
   ```
   Replace with:
   ```go
   if err := a.underlying.Write(context.Background(), e); err != nil {
       if a.OnError != nil {
           a.OnError(e, err)
       }
   }
   ```

**Test to write**
File: `go-core/audit/async_test.go` (read first).
Append:

```go
func TestAsyncRepository_OnError_CalledOnWriteFailure(t *testing.T) {
    writeErr := fmt.Errorf("intentional write failure")
    failRepo := &fakeRepo{
        writeFn: func(_ context.Context, _ Entry) error {
            return writeErr
        },
    }

    var gotEntry Entry
    var gotErr error
    callbackCalled := make(chan struct{}, 1)

    ar := NewAsyncRepository(failRepo, 10, 1)
    ar.OnError = func(e Entry, err error) {
        gotEntry = e
        gotErr = err
        callbackCalled <- struct{}{}
    }

    e := NewEntry("user", 1, 1, "create")
    if err := ar.Write(context.Background(), e); err != nil {
        t.Fatalf("Write: %v", err)
    }

    select {
    case <-callbackCalled:
    case <-time.After(time.Second):
        t.Fatal("OnError was not called within 1s")
    }

    if gotErr == nil || gotErr.Error() != writeErr.Error() {
        t.Fatalf("expected error %q, got %v", writeErr, gotErr)
    }
    if gotEntry.Action != "create" {
        t.Fatalf("expected entry with action 'create', got %q", gotEntry.Action)
    }

    ar.Shutdown(context.Background())
}
```

Add `"fmt"` to the test file imports if not present.

**Verification commands**
```
go build ./...
go test -race ./audit/...
go test ./...
go vet ./...
```

---

### Task 8.4 — Add `EventBusHealthChecker` to `health/` package; wire in bootstrap [x]

**Problem**
The health registry only checks database connectivity. A NATS connectivity
failure or a broken event bus is invisible to load balancers and monitoring.
An `EventBusHealthChecker` completes the health picture.

This task depends on Task 7.2 (health package created) and Task 8.1 (NATSBus
has a logger). Complete both before starting this task.

**Files to read first**
1. `go-core/health/health.go` — read the ENTIRE file (created in Task 7.2)
2. `go-core/event/bus.go` — read the ENTIRE file to understand the Bus interface
3. `go-core/bootstrap/builder.go` — read `setupHealth()` and `setupBus()`

**Exact steps**
1. Open `go-core/health/health.go`.
2. The `Bus` interface from `event/bus.go` has `Publish` and `Subscribe`. There
   is no `Ping` method. Add a `Pinger` interface to the health package (not to
   event/bus.go):
   ```go
   // Pinger is an optional interface that event bus implementations may
   // satisfy to support health checking.
   type Pinger interface {
       Ping(ctx context.Context) error
   }
   ```
3. Add a new `EventBusChecker` struct and constructor:
   ```go
   // EventBusChecker checks event bus connectivity. It expects the bus to
   // implement the Pinger interface. If it does not, Check always returns nil
   // (no-op — the bus does not support health checks).
   type EventBusChecker struct {
       bus any // stored as any to avoid importing event package
   }

   // NewEventBusChecker wraps a bus for health checking.
   // If bus implements Pinger, its Ping method is called on each check.
   func NewEventBusChecker(bus any) *EventBusChecker {
       return &EventBusChecker{bus: bus}
   }

   // Check calls Ping on the bus if it implements Pinger.
   func (e *EventBusChecker) Check(ctx context.Context) error {
       if p, ok := e.bus.(Pinger); ok {
           return p.Ping(ctx)
       }
       return nil // bus does not support Ping — health check is a no-op
   }
   ```
4. Add `Ping(ctx context.Context) error` to `event/nats_adapter.go` on `NATSBus`:
   ```go
   // Ping verifies the NATS connection by publishing a zero-byte probe to a
   // dedicated health subject. Returns an error if the client is disconnected.
   func (n *NATSBus) Ping(ctx context.Context) error {
       return n.client.Publish("_health.ping", nil)
   }
   ```
   The `InMemoryBus` in `event/bus.go` does NOT get a Ping method — it is
   always healthy by definition.
5. Open `go-core/bootstrap/builder.go`.
   In `setupHealth()`, after adding `NewDBHealthChecker`, also add the event bus
   checker:
   ```go
   if bus, err := Resolve[event.Bus](b.container); err == nil {
       hr.Add(health.NewEventBusChecker(bus))
   }
   ```
   Add import `"github.com/wssto2/go-core/health"` if not already present
   (it was added in Task 7.2). Also ensure `"github.com/wssto2/go-core/event"`
   is imported.

**Test to write**
File: `go-core/health/health_test.go` (read first).
Append:

```go
type fakePinger struct {
    err error
}

func (f *fakePinger) Ping(_ context.Context) error { return f.err }

func TestEventBusChecker_WithPinger_CallsPing(t *testing.T) {
    checker := NewEventBusChecker(&fakePinger{err: nil})
    if err := checker.Check(context.Background()); err != nil {
        t.Fatalf("expected nil error from healthy pinger, got %v", err)
    }
}

func TestEventBusChecker_WithPinger_PropagatesPingError(t *testing.T) {
    pingErr := fmt.Errorf("nats disconnected")
    checker := NewEventBusChecker(&fakePinger{err: pingErr})
    err := checker.Check(context.Background())
    if err == nil {
        t.Fatal("expected error from failing pinger, got nil")
    }
}

func TestEventBusChecker_WithoutPinger_ReturnsNil(t *testing.T) {
    // A plain struct that does not implement Pinger.
    checker := NewEventBusChecker(struct{}{})
    if err := checker.Check(context.Background()); err != nil {
        t.Fatalf("expected nil for non-Pinger bus, got %v", err)
    }
}
```

Add `"fmt"` and `"context"` to test imports if not present.

**Verification commands**
```
go build ./...
go test ./health/...
go test ./event/...
go test ./bootstrap/...
go test ./...
go vet ./...
```

---

## Final Phase Completion Markers

* [x] Phase 1 — Critical Bug Fixes    (Tasks 1.1–1.6)
* [x] Phase 2 — Dead Code Removal     (Tasks 2.1–2.5)
* [x] Phase 3 — Duplicate Elimination (Tasks 3.1–3.3)
* [x] Phase 4 — Performance           (Tasks 4.1–4.3)
* [x] Phase 5 — DI Container          (Task 5.1)
* [x] Phase 6 — Naming and DX         (Tasks 6.1–6.5)
* [x] Phase 7 — Logic Relocation      (Tasks 7.1–7.4)
* [x] Phase 8 — Feature Additions     (Tasks 8.1–8.4)
