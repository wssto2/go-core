package cache

import (
	"context"
	"errors"
	"sync"
)

var ErrCacheMiss = errors.New("cache: key not found")

// Cache defines the interface for a simple key-value store.
type Cache interface {
	Set(ctx context.Context, key string, value any) error
	Get(ctx context.Context, key string) (any, error)
	Delete(ctx context.Context, key string) error
}

// InMemoryCache is an in-memory implementation of the Cache interface.
type InMemoryCache struct {
	mu   sync.RWMutex
	data map[string]any
}

func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		data: make(map[string]any),
	}
}

func (c *InMemoryCache) Set(_ context.Context, key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = value
	return nil
}

func (c *InMemoryCache) Get(_ context.Context, key string) (any, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[key]
	if !ok {
		return nil, ErrCacheMiss
	}
	return v, nil
}

func (c *InMemoryCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
	return nil
}
