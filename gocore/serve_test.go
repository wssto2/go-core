//nolint:testpackage // uses the package's runnable and waitFor helpers
package gocore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// serving runs app on a free port and returns its base URL; the app stops with the test.
func serving(t *testing.T, app *App, port int) string {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- app.RunContext(ctx) }()

	t.Cleanup(func() {
		cancel()

		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	})

	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	waitFor(t, "the server", func() bool {
		select {
		case err := <-done:
			t.Fatalf("Run ended early: %v", err)
		default:
		}

		resp, err := get(base + "/health")
		if err != nil {
			return false
		}

		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusOK
	})

	return base
}

// fetch returns the status, headers and body of GET url.
func fetch(t *testing.T, url string, headers ...string) (*http.Response, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	return resp, strings.TrimSpace(string(body))
}
