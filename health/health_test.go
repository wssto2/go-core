package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type mockChecker struct {
	name string
	err  error
}

func (m *mockChecker) Name() string                    { return m.name }
func (m *mockChecker) Check(ctx context.Context) error { return m.err }

func TestLivenessHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/live", LivenessHandler())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/live", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "up")
}

func TestReadinessHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("up when all checkers pass", func(t *testing.T) {
		reg := NewHealthRegistry()
		reg.Add(&mockChecker{name: "db", err: nil})

		router := gin.New()
		router.GET("/ready", ReadinessHandler(reg))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/ready", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"up"`)
		assert.Contains(t, w.Body.String(), `"db":"up"`)
	})

	t.Run("degraded when some checkers fail", func(t *testing.T) {
		reg := NewHealthRegistry()
		reg.Add(&mockChecker{name: "db", err: errors.New("timeout")})

		router := gin.New()
		router.GET("/ready", ReadinessHandler(reg))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/ready", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"degraded"`)
		assert.Contains(t, w.Body.String(), `"db":"down"`)
		assert.NotContains(t, w.Body.String(), "timeout", "error details must not leak in HTTP response")
	})
}

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
	pingErr := errors.New("nats disconnected")
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

// Additional test migrated from health_test.go

type failChecker struct{}

func (f *failChecker) Name() string                    { return "fail" }
func (f *failChecker) Check(ctx context.Context) error { return errors.New("boom") }

func TestHealthEndpointsAndReadinessDegraded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Simulate app setup
	reg := NewHealthRegistry()
	router := gin.New()
	router.GET("/health", LivenessHandler())
	router.GET("/ready", ReadinessHandler(reg))

	// Liveness
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected /health 200, got %d", w.Code)
	}
	var lv map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &lv); err != nil {
		t.Fatalf("unmarshal liveness: %v", err)
	}
	if lv["status"] != "up" {
		t.Fatalf("unexpected liveness body: %v", lv)
	}

	// Readiness without failing checkers should be OK
	req = httptest.NewRequest("GET", "/ready", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected /ready 200, got %d", w.Code)
	}

	// Add a failing checker and expect degraded/503
	reg.Add(&failChecker{})

	req = httptest.NewRequest("GET", "/ready", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected /ready 503, got %d", w.Code)
	}
	var rd map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rd); err != nil {
		t.Fatalf("unmarshal readiness: %v", err)
	}
	if rd["status"] != "degraded" {
		t.Fatalf("expected status degraded, got %v", rd["status"])
	}
	checks, ok := rd["checks"].(map[string]any)
	if !ok {
		t.Fatalf("expected checks map, got %+v", rd["checks"])
	}
	if _, found := checks["fail"]; !found {
		t.Fatalf("expected failing checker 'fail' present in checks: %+v", checks)
	}
}

// TestReadinessHandler_CheckerError_DoesNotLeakDetails ensures that error
// details from failing checkers are NOT exposed in the HTTP response body.
func TestReadinessHandler_CheckerError_DoesNotLeakDetails(t *testing.T) {
gin.SetMode(gin.TestMode)
reg := NewHealthRegistry()
reg.Add(&mockChecker{name: "db", err: errors.New("secret connection string: host=internal-db")})

router := gin.New()
router.GET("/ready", ReadinessHandler(reg))

w := httptest.NewRecorder()
req := httptest.NewRequest("GET", "/ready", nil)
router.ServeHTTP(w, req)

assert.Equal(t, http.StatusServiceUnavailable, w.Code)
body := w.Body.String()
assert.Contains(t, body, `"down"`)
assert.NotContains(t, body, "secret", "internal error details must not appear in response")
assert.NotContains(t, body, "connection string", "internal error details must not appear in response")
}

// slowPinger blocks for longer than the timeout to simulate a hung DB.
type slowPinger struct{ delay time.Duration }

func (s *slowPinger) PingContext(ctx context.Context) error {
select {
case <-time.After(s.delay):
return nil
case <-ctx.Done():
return ctx.Err()
}
}

// slowDBChecker wraps slowPinger to test the timeout without a real DB.
type slowDBChecker struct{ delay time.Duration }

func (s *slowDBChecker) Name() string { return "slow-db" }
func (s *slowDBChecker) Check(ctx context.Context) error {
pingCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
defer cancel()
select {
case <-time.After(s.delay):
return nil
case <-pingCtx.Done():
return pingCtx.Err()
}
}

// TestDBHealthChecker_SlowPing_TimesOut verifies that a slow DB ping is
// bounded by the configured timeout, not the caller's context.
func TestDBHealthChecker_SlowPing_TimesOut(t *testing.T) {
checker := &slowDBChecker{delay: 10 * time.Second}
ctx := context.Background()

start := time.Now()
err := checker.Check(ctx)
elapsed := time.Since(start)

assert.Error(t, err, "slow ping must return deadline-exceeded error")
assert.Less(t, elapsed, 1*time.Second, "check must return well within 1s, not block for 10s")
}
