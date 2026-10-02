// Package gocoretest builds a gocore.App for tests: in-memory SQLite, a fixed
// clock and a logger that writes to the test log. Features are installed with
// plain function calls, and routes are called without opening a port.
//
//	app := gocoretest.New(t)
//	tickets.Install(app, users)
//
//	rec := gocoretest.Do(t, app, http.MethodGet, "/tickets/7", nil)
//	ticket := gocoretest.Decode[Ticket](t, rec)
package gocoretest

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
)

// Epoch is the time a test App's clock shows unless At says otherwise.
var Epoch = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

type settings struct {
	now        time.Time
	databases  []string
	authorizer authz.Authorizer
}

// Option adjusts New.
type Option func(*settings)

// At sets the time the App's clock shows.
func At(now time.Time) Option { return func(s *settings) { s.now = now } }

// Databases names the in-memory connections to register. The first is the
// primary one. The default is a single connection called "local".
func Databases(names ...string) Option { return func(s *settings) { s.databases = names } }

// Authorizer sets who may do what. Without it, routes that require a
// permission fail the application check.
func Authorizer(a authz.Authorizer) Option { return func(s *settings) { s.authorizer = a } }

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

// New returns an App on in-memory SQLite, with a fixed clock and the test log
// as its logger. The databases close when the test ends.
func New(t testing.TB, opts ...Option) *gocore.App {
	t.Helper()

	s := settings{now: Epoch, databases: []string{"local"}}
	for _, opt := range opts {
		opt(&s)
	}

	reg, cleanup := database.NewTestRegistry(s.databases...)
	t.Cleanup(func() { _ = cleanup() })

	appOpts := []gocore.Option{
		gocore.WithRegistry(reg),
		gocore.WithClock(fixedClock(s.now)),
		gocore.WithLogger(slog.New(slog.NewTextHandler(testLog{t}, nil))),
	}
	if s.authorizer != nil {
		appOpts = append(appOpts, gocore.WithAuthorizer(s.authorizer))
	}

	return gocore.New(bootstrap.DefaultConfig(), appOpts...)
}

type testLog struct{ t testing.TB }

func (l testLog) Write(p []byte) (int, error) {
	l.t.Log(string(bytes.TrimRight(p, "\n")))
	return len(p), nil
}

// Do calls an installed route as an HTTP request, without opening a port. A
// non-nil body is sent as JSON. The test fails if the application check does.
func Do(t testing.TB, app *gocore.App, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	handler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}

		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequestWithContext(t.Context(), method, path, reader)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

// Decode reads the "data" of a successful response into T. The test fails on
// a status outside 2xx or a body that does not decode.
func Decode[T any](t testing.TB, rec *httptest.ResponseRecorder) T {
	t.Helper()

	if rec.Code < http.StatusOK || rec.Code >= http.StatusMultipleChoices {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	var envelope struct {
		Data T `json:"data"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body, err)
	}

	return envelope.Data
}
