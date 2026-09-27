package bootstrap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// freePort returns a TCP port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}
	return port
}

// startRealServer builds an App with a real *http.Server (WithHttp), lets the
// caller add routes, starts it and waits until it accepts connections.
func startRealServer(t *testing.T, cfg Config, routes func(e *gin.Engine)) (*App, string) {
	t.Helper()
	cfg.I18n.Dir = tempI18nDir(t)
	cfg.HTTP.Port = freePort(t)
	app, err := New(cfg).DefaultInfrastructure().WithHttp().Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	routes(app.engine)

	go func() {
		// ErrServerClosed after Shutdown is the expected outcome.
		_ = app.httpServer.Start()
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.HTTP.Port)
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, dialErr := net.Dial("tcp", addr)
		if dialErr == nil {
			_ = conn.Close() // probe connection only
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start: %v", dialErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return app, "http://" + addr
}

// A long-lived request (Server-Sent Events) ends only when its request context
// ends. Shutdown must cancel it after the grace period instead of waiting for
// the whole shutdown timeout.
func TestShutdown_EndsLongLivedRequestsAfterGracePeriod(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HTTP.ShutdownTimeout = 10 * time.Second
	cfg.HTTP.ShutdownGracePeriod = 200 * time.Millisecond

	entered := make(chan struct{})
	handlerDone := make(chan struct{})
	app, base := startRealServer(t, cfg, func(e *gin.Engine) {
		e.GET("/stream", func(c *gin.Context) {
			defer close(handlerDone)
			c.Header("Content-Type", "text/event-stream")
			c.Status(http.StatusOK)
			c.Writer.Flush()
			close(entered)
			<-c.Request.Context().Done()
		})
	})

	go func() {
		resp, err := http.Get(base + "/stream") //nolint:noctx // test client
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body) // drain until the server ends the stream
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler never started")
	}

	start := time.Now()
	app.Shutdown(slog.Default())
	elapsed := time.Since(start)

	select {
	case <-handlerDone:
	default:
		t.Fatal("Shutdown returned while the long-lived request was still running")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("Shutdown took %v; a long-lived request must end shortly after the grace period", elapsed)
	}
}

// A normal in-flight request that finishes within the grace period is drained,
// not cut: it completes, and Shutdown waits for it.
func TestShutdown_DrainsShortInFlightRequests(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HTTP.ShutdownTimeout = 10 * time.Second
	cfg.HTTP.ShutdownGracePeriod = 2 * time.Second

	entered := make(chan struct{})
	app, base := startRealServer(t, cfg, func(e *gin.Engine) {
		e.GET("/slow", func(c *gin.Context) {
			close(entered)
			time.Sleep(300 * time.Millisecond)
			if c.Request.Context().Err() != nil {
				c.String(http.StatusServiceUnavailable, "cancelled")
				return
			}
			c.String(http.StatusOK, "done")
		})
	})

	type result struct {
		status int
		body   string
		at     time.Time
		err    error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := http.Get(base + "/slow") //nolint:noctx // test client
		if err != nil {
			resCh <- result{err: err}
			return
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		resCh <- result{status: resp.StatusCode, body: string(b), at: time.Now()}
	}()

	<-entered
	app.Shutdown(slog.Default())
	returned := time.Now()

	res := <-resCh
	if res.err != nil {
		t.Fatalf("in-flight request failed: %v", res.err)
	}
	if res.status != http.StatusOK || res.body != "done" {
		t.Fatalf("in-flight request was not drained: status %d body %q", res.status, res.body)
	}
	if returned.Before(res.at.Add(-50 * time.Millisecond)) {
		t.Fatalf("Shutdown returned %v before the in-flight request finished", res.at.Sub(returned))
	}
}

// ShutdownTimeout is a time.Duration; the modules must get a context that
// is still alive, with a deadline about ShutdownTimeout away.
func TestShutdown_ModulesGetTheConfiguredTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.I18n.Dir = tempI18nDir(t)
	cfg.HTTP.ShutdownTimeout = 10 * time.Second
	app, err := New(cfg).DefaultInfrastructure().Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	app.httpServer = &fakeHTTPServer{}

	var ctxErr error
	var remaining time.Duration
	app.modules = []Module{&ctxModule{fn: func(ctx context.Context) {
		ctxErr = ctx.Err()
		if dl, ok := ctx.Deadline(); ok {
			remaining = time.Until(dl)
		}
	}}}

	app.Shutdown(slog.Default())

	if ctxErr != nil {
		t.Fatalf("module shutdown context already done: %v", ctxErr)
	}
	if remaining < 9*time.Second || remaining > 10*time.Second {
		t.Fatalf("module shutdown deadline %v away, want about 10s", remaining)
	}
}

type ctxModule struct {
	fn func(ctx context.Context)
}

func (m *ctxModule) Name() string               { return "ctx" }
func (m *ctxModule) Register(*Container) error  { return nil }
func (m *ctxModule) Boot(context.Context) error { return nil }
func (m *ctxModule) Shutdown(ctx context.Context) error {
	m.fn(ctx)
	return nil
}
