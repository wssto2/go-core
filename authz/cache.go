package authz

import (
	"sync"
	"time"
)

// DefaultCacheTTL is the safety-net lifetime of a cached effective access. The
// real invalidation is explicit (Evict, EvictRole); the TTL only bounds how long
// another process's change can go unnoticed.
const DefaultCacheTTL = 5 * time.Minute

// Cache holds the effective access of each subject and evicts it explicitly
// when a role or binding changes, so a change applies on the next request. It
// is safe for concurrent use.
//
// Every eviction advances an epoch. A load that started before an eviction
// cannot store its result afterwards, so a slow reader never resurrects stale
// access.
//
// The cache is per process. With several instances, evict on each (for example
// from an event) and rely on the TTL as the backstop.
type Cache struct {
	mu      sync.Mutex
	entries map[Subject]cacheEntry
	epoch   uint64
	ttl     time.Duration
	now     func() time.Time
}

type cacheEntry struct {
	eff     *Effective
	expires time.Time
}

// NewCache returns a cache whose entries expire after ttl (DefaultCacheTTL when
// ttl is not positive).
func NewCache(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &Cache{entries: make(map[Subject]cacheEntry), ttl: ttl, now: time.Now}
}

func (c *Cache) get(s Subject) (*Effective, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[s]
	if !ok {
		return nil, false
	}
	if !c.now().Before(e.expires) {
		delete(c.entries, s)
		return nil, false
	}
	return e.eff, true
}

// begin returns the epoch a load must present to put.
func (c *Cache) begin() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.epoch
}

func (c *Cache) put(s Subject, eff *Effective, epoch uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if epoch != c.epoch {
		return // evicted while loading: the result may be stale
	}
	c.entries[s] = cacheEntry{eff: eff, expires: c.now().Add(c.ttl)}
}

// Evict drops one subject's cached access.
func (c *Cache) Evict(s Subject) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	delete(c.entries, s)
}

// EvictRole drops the cached access of every subject holding the custom role.
func (c *Cache) EvictRole(roleID int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	for s, e := range c.entries {
		if e.eff.usesRole(roleID) {
			delete(c.entries, s)
		}
	}
}

// EvictAll empties the cache: after a predefined role or the scope hierarchy
// changes.
func (c *Cache) EvictAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	clear(c.entries)
}

// Len is the number of cached subjects.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
