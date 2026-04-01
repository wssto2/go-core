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

// IdempotencyStore stores responses for idempotency keys.
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
func Idempotency(store *IdempotencyStore) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		key := ctx.GetHeader(HeaderIdempotencyKey)
		if key == "" {
			ctx.Next()
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

			ctx.Next()

			// after handler finished, record response
			status := cw.Status()
			head := cw.Header().Clone()
			body := cw.body.Bytes()
			store.setResponse(key, status, head, body)
			return
		}

		// wait for the in-progress request to finish or context to cancel
		e, err := store.waitResponse(entry, ctx.Request.Context())
		if err != nil {
			// propagate context error
			ctx.AbortWithStatusJSON(http.StatusRequestTimeout, gin.H{"error": err.Error()})
			return
		}

		writeStoredResponse(ctx, e)
		ctx.Abort()
	}
}

func writeStoredResponse(ctx *gin.Context, e *idEntry) {
	for k, vals := range e.head {
		for _, v := range vals {
			ctx.Writer.Header().Add(k, v)
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
