package bootstrap

import (
	"context"
	"net"
	"net/http"
	"time"
)

// HTTPServer is an abstraction used by the App to start and shutdown the HTTP server.
// This allows tests to provide a fake implementation for graceful shutdown verification.
type HTTPServer interface {
	Start() error
	Shutdown(ctx context.Context) error
}

// serverWrapper wraps a standard *http.Server to implement HTTPServer.
//
// Every request context derives from a base context the wrapper owns. Shutdown
// first drains gracefully (http.Server.Shutdown: stop accepting, wait for
// in-flight requests); if requests are still running after gracePeriod, it
// cancels the base context so long-lived requests (Server-Sent Events, long
// polling), which only end when their context ends, return instead of holding
// Shutdown for the whole timeout. The trade-off: a normal request still
// running after the grace period sees its context cancelled too.
type serverWrapper struct {
	srv         *http.Server
	cancelBase  context.CancelFunc
	gracePeriod time.Duration
}

// newServerWrapper installs the cancellable base context on srv.
func newServerWrapper(srv *http.Server, gracePeriod time.Duration) *serverWrapper {
	// The base context outlives any caller: it is the root of every request
	// context and is cancelled only by Shutdown.
	baseCtx, cancel := context.WithCancel(context.Background())
	srv.BaseContext = func(net.Listener) context.Context { return baseCtx }

	return &serverWrapper{srv: srv, cancelBase: cancel, gracePeriod: gracePeriod}
}

func (s *serverWrapper) Start() error {
	return s.srv.ListenAndServe()
}

func (s *serverWrapper) Shutdown(ctx context.Context) error {
	done := make(chan error, 1)
	go func() { done <- s.srv.Shutdown(ctx) }()

	grace := time.NewTimer(s.gracePeriod)
	defer grace.Stop()

	select {
	case err := <-done:
		// Drained (or ctx ended): release anything still holding a request context.
		s.cancelBase()

		return err
	case <-grace.C:
	}

	// Requests are still running after the grace period: end their contexts
	// so long-lived handlers return, then let Shutdown finish the drain.
	s.cancelBase()

	return <-done
}
