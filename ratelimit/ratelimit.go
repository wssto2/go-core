package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter defines the minimal rate limiter interface used by middleware and services.
type Limiter interface {
	// Allow checks whether the given key is allowed to proceed. Returns (true, nil)
	// when allowed. On internal failure returns (false, *apperr.AppError).
	Allow(ctx context.Context, key string) (bool, error)
}

// In-memory fixed-window limiter (fallback).
type InMemoryLimiter struct {
	limit  int
	window time.Duration
	mu     sync.Mutex
	m      map[string]*memCounter
	stopCh chan struct{}
}

type memCounter struct {
	count  int
	expiry time.Time
}

// NewInMemoryLimiter constructs a simple in-memory limiter. Not suitable for
// multi-instance deployments but useful as a local fallback and for tests.
// Call Stop() when the limiter is no longer needed to release the background goroutine.
func NewInMemoryLimiter(limit int, window time.Duration) *InMemoryLimiter {
	if limit <= 0 {
		limit = 1
	}
	if window <= 0 {
		window = time.Second
	}
	l := &InMemoryLimiter{
		limit:  limit,
		window: window,
		m:      make(map[string]*memCounter),
		stopCh: make(chan struct{}),
	}
	go l.sweep()
	return l
}

// sweep periodically removes expired window entries to prevent unbounded map growth.
func (l *InMemoryLimiter) sweep() {
	ticker := time.NewTicker(l.window)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			l.mu.Lock()
			for k, c := range l.m {
				if now.After(c.expiry) {
					delete(l.m, k)
				}
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

// Stop terminates the background sweep goroutine. It is safe to call multiple times.
func (l *InMemoryLimiter) Stop() {
	select {
	case <-l.stopCh: // already stopped
	default:
		close(l.stopCh)
	}
}

// Len returns the current number of tracked keys. Primarily for testing.
func (l *InMemoryLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.m)
}
func (l *InMemoryLimiter) Allow(ctx context.Context, key string) (bool, error) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	c, ok := l.m[key]
	if !ok || now.After(c.expiry) {
		// start a new window
		l.m[key] = &memCounter{count: 1, expiry: now.Add(l.window)}
		return true, nil
	}

	if c.count < l.limit {
		c.count++
		return true, nil
	}

	return false, nil
}

