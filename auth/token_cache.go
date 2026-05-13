package auth

import (
	"sync"
	"time"
)

// cachedEntry holds a resolved identity and the instant it should be evicted.
type cachedEntry struct {
	user      Identifiable
	tokenID   int
	expiresAt time.Time // min(now+cacheTTL, token.ExpiresAt) — never outlives the real token
}

// tokenCache is a short-lived, write-through cache keyed by raw token value.
// It is safe for concurrent use: reads acquire RLock, writes acquire Lock,
// and the background sweep acquires Lock only during the delete phase.
type tokenCache struct {
	mu      sync.RWMutex
	entries map[string]cachedEntry
	ttl     time.Duration

	stopOnce sync.Once
	stop     chan struct{}
}

func newTokenCache(ttl time.Duration) *tokenCache {
	c := &tokenCache{
		entries: make(map[string]cachedEntry),
		ttl:     ttl,
		stop:    make(chan struct{}),
	}
	go c.sweep()
	return c
}

// get returns the cached entry for token, or (zero, false) if absent or expired.
// Expired entries are treated as misses; they are removed lazily by the sweep.
func (c *tokenCache) get(token string) (cachedEntry, bool) {
	c.mu.RLock()
	e, ok := c.entries[token]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		return cachedEntry{}, false
	}
	return e, true
}

// set inserts or replaces the entry for token.
// expiresAt is clamped to min(now+ttl, tokenExpiry) so we never serve a user
// whose underlying DB token has already expired.
func (c *tokenCache) set(token string, tokenID int, user Identifiable, tokenExpiry time.Time) {
	cacheExpiry := time.Now().Add(c.ttl)
	if tokenExpiry.Before(cacheExpiry) {
		cacheExpiry = tokenExpiry
	}
	c.mu.Lock()
	c.entries[token] = cachedEntry{user: user, tokenID: tokenID, expiresAt: cacheExpiry}
	c.mu.Unlock()
}

// delete removes a single entry — called on explicit token revocation (logout).
func (c *tokenCache) delete(token string) {
	c.mu.Lock()
	delete(c.entries, token)
	c.mu.Unlock()
}

// sweep removes expired entries on each tick. It runs until Stop is called.
func (c *tokenCache) sweep() {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			// Collect keys under RLock, then delete under Lock.
			// Avoids holding the write lock while iterating a potentially large map.
			c.mu.RLock()
			var expired []string
			for k, e := range c.entries {
				if now.After(e.expiresAt) {
					expired = append(expired, k)
				}
			}
			c.mu.RUnlock()

			if len(expired) > 0 {
				c.mu.Lock()
				for _, k := range expired {
					// Re-check under write lock: a concurrent set may have refreshed the entry.
					if e, ok := c.entries[k]; ok && now.After(e.expiresAt) {
						delete(c.entries, k)
					}
				}
				c.mu.Unlock()
			}
		case <-c.stop:
			return
		}
	}
}

// Stop shuts down the background sweep goroutine. Safe to call multiple times.
func (c *tokenCache) Stop() {
	c.stopOnce.Do(func() { close(c.stop) })
}
