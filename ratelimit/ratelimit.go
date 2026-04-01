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
}

type memCounter struct {
	count  int
	expiry time.Time
}

// NewInMemoryLimiter constructs a simple in-memory limiter. Not suitable for
// multi-instance deployments but useful as a local fallback and for tests.
func NewInMemoryLimiter(limit int, window time.Duration) *InMemoryLimiter {
	if limit <= 0 {
		limit = 1
	}
	if window <= 0 {
		window = time.Second
	}
	return &InMemoryLimiter{
		limit:  limit,
		window: window,
		m:      make(map[string]*memCounter),
	}
}

// Allow implements the limiter. It increments a per-key counter within a fixed window.
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
