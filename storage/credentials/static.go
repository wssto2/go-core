package credentials

import (
	"context"
	"sync"

	"github.com/wssto2/go-core/apperr"
)

// StaticResolver is an in-memory credentials resolver suitable for tests and simple configs.
type StaticResolver struct {
	mu    sync.RWMutex
	store map[string]Credentials
}

// NewStaticResolver creates a resolver pre-populated with the given map.
func NewStaticResolver(initial map[string]Credentials) *StaticResolver {
	s := make(map[string]Credentials, len(initial))
	for k, v := range initial {
		s[k] = v
	}
	return &StaticResolver{store: s}
}

// Resolve returns credentials for the given name or an apperr.NotFound if missing.
func (s *StaticResolver) Resolve(ctx context.Context, name string) (*Credentials, error) {
	if name == "" {
		return nil, apperr.BadRequest("credential name is empty")
	}
	s.mu.RLock()
	c, ok := s.store[name]
	s.mu.RUnlock()
	if !ok {
		return nil, apperr.NotFound("credentials not found")
	}
	// return a copy
	cc := c
	return &cc, nil
}

// Set adds or updates credentials for a name.
func (s *StaticResolver) Set(name string, c Credentials) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store == nil {
		s.store = make(map[string]Credentials)
	}
	s.store[name] = c
}
