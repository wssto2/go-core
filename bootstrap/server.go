package bootstrap

import (
	"context"
	"net/http"
)

// HTTPServer is an abstraction used by the App to start and shutdown the HTTP server.
// This allows tests to provide a fake implementation for graceful shutdown verification.
type HTTPServer interface {
	Start() error
	Shutdown(ctx context.Context) error
}

// serverWrapper wraps a standard *http.Server to implement HTTPServer.
type serverWrapper struct {
	srv *http.Server
}

func (s *serverWrapper) Start() error {
	return s.srv.ListenAndServe()
}

func (s *serverWrapper) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
