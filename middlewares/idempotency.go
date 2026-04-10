// Package middlewares provides HTTP middleware for the gin framework.
//
// # Idempotency (HTTP level)
//
// The Idempotency middleware deduplicates HTTP requests that share the same
// Idempotency-Key header. It captures the full HTTP response (status line,
// headers, body) on the first request and replays the stored bytes to any
// subsequent request with the same key.
//
// This is a fundamentally different concern from event-level deduplication
// (see event.ProcessedStore / event.DBProcessedStore): the HTTP middleware
// operates on serialised HTTP responses, while event-level stores track only
// whether an event identifier has been processed.
package middlewares

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const HeaderIdempotencyKey = "Idempotency-Key"

// in-memory idempotency store
type idEntry struct {
	ch     chan struct{}
	done   bool
	status int
	head   http.Header
	body   []byte
	// optional TTL timer
	timer *time.Timer
}

// IdempotencyStore stores captured HTTP responses keyed by idempotency key.
// It is safe for concurrent use. Use NewInMemoryIdempotencyStore to construct one.
type IdempotencyStore struct {
	mu  sync.Mutex
	m   map[string]*idEntry
	ttl time.Duration
}

// NewInMemoryIdempotencyStore creates a store with optional ttl for entries (0 = no eviction).
func NewInMemoryIdempotencyStore(ttl time.Duration) *IdempotencyStore {
	return &IdempotencyStore{m: make(map[string]*idEntry), ttl: ttl}
}

func (s *IdempotencyStore) getOrCreate(key string) (*idEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.m[key]; ok {
		return e, false
	}
	e := &idEntry{ch: make(chan struct{})}
	s.m[key] = e
	return e, true
}

func (s *IdempotencyStore) setResponse(key string, status int, header http.Header, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok {
		// shouldn't happen but guard
		e = &idEntry{ch: make(chan struct{})}
		s.m[key] = e
	}
	e.status = status
	e.head = header.Clone()
	e.body = append([]byte(nil), body...)
	e.done = true
	close(e.ch)
	if s.ttl > 0 {
		// schedule eviction
		if e.timer != nil {
			e.timer.Stop()
		}
		e.timer = time.AfterFunc(s.ttl, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.m, key)
		})
	}
}

// waitResponse waits for entry to be done or context canceled. Returns entry and nil error when done.
// If ctx is canceled before done, returns (nil, ctx.Err()).
func (s *IdempotencyStore) waitResponse(e *idEntry, ctx context.Context) (*idEntry, error) {
	select {
	case <-e.ch:
		return e, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Idempotency middleware accepts an Idempotency-Key header and ensures duplicate
// requests with the same key receive the same stored response and do not re-run handlers.
//
// Clients supply an Idempotency-Key header (max 256 bytes) with any mutating
// request (POST, PUT, PATCH). On the first request the full HTTP response is
// captured and stored. Any subsequent request with the same key receives the
// stored response immediately without executing the handler again.
//
// This prevents accidental double-mutations caused by network retries — e.g. a
// client that times out and retries a "create order" POST will get back the
// original 201 response instead of a duplicate record.
//
// Use NewInMemoryIdempotencyStore with an appropriate TTL (e.g. 24h) and apply
// the middleware to mutating route groups:
//
//	store := middlewares.NewInMemoryIdempotencyStore(24 * time.Hour)
//	api.POST("/orders",
//	    middlewares.Idempotency(store),
//	    handler.CreateOrder,
//	)
//
// This is HTTP-level deduplication. For event-level deduplication see
// event.ProcessedStore / event.DBProcessedStore.
func Idempotency(store *IdempotencyStore) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		key := ctx.GetHeader(HeaderIdempotencyKey)
		if key == "" {
			ctx.Next()
			return
		}

		// Task 6.2: reject oversized keys before touching the store.
		if len(key) > 256 {
			ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "Idempotency-Key too long (max 256 bytes)",
			})
			return
		}

		entry, created := store.getOrCreate(key)
		if created {
			// first request: capture response
			buf := &bytes.Buffer{}
			// wrap writer
			w := ctx.Writer
			cw := &captureWriter{ResponseWriter: w, body: buf}
			ctx.Writer = cw

			// Always call setResponse, even if the handler panics.
			// This closes the channel so waiting duplicates are not blocked forever.
			// Restore the original writer so gin's recovery middleware sees the real writer.
			defer func() {
				ctx.Writer = w
				store.setResponse(key, cw.Status(), cw.Header(), cw.body.Bytes())
			}()

			ctx.Next()
			return
		}

		// wait for the in-progress request to finish or context to cancel
		e, err := store.waitResponse(entry, ctx.Request.Context())
		if err != nil {
			// propagate context error
			ctx.AbortWithStatusJSON(http.StatusRequestTimeout, gin.H{
				"success": false,
				"error":   err.Error(),
			})
			return
		}

		writeStoredResponse(ctx, e)
		ctx.Abort()
	}
}

func writeStoredResponse(ctx *gin.Context, e *idEntry) {
	for k, vals := range e.head {
		for i, v := range vals {
			if i == 0 {
				ctx.Writer.Header().Set(k, v)
			} else {
				ctx.Writer.Header().Add(k, v)
			}
		}
	}
	ctx.Writer.WriteHeader(e.status)
	// ignore write error in middleware
	_, _ = ctx.Writer.Write(e.body)
}

// captureWriter wraps gin.ResponseWriter to capture body writes.
type captureWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (c *captureWriter) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	if n > 0 {
		_, _ = c.body.Write(b[:n])
	}
	return n, err
}

// ensure captureWriter implements http.ResponseWriter (gin.ResponseWriter does)
var _ gin.ResponseWriter = (*captureWriter)(nil)
