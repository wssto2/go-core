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

		err := <-done
		if err != nil {
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

// reply is what a request got back.
type reply struct {
	status int
	header http.Header
	body   string
}

// fetch returns the answer to GET url.
func fetch(t *testing.T, url string) reply {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	err = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	return reply{status: resp.StatusCode, header: resp.Header, body: strings.TrimSpace(string(body))}
}
