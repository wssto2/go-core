package authz

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func effWithRole(id int) *Effective {
	return &Effective{roleIDs: map[int]struct{}{id: {}}, grants: map[string][]Clause{}}
}

func TestCacheEvictionRules(t *testing.T) {
	c := NewCache(time.Minute)
	a, b, d := Subject{KindUser, 1}, Subject{KindUser, 2}, Subject{KindServiceAccount, 1}
	fill := func() {
		c.put(a, effWithRole(10), c.begin())
		c.put(b, effWithRole(20), c.begin())
		c.put(d, effWithRole(10), c.begin())
	}

	fill()
	assert.Equal(t, 3, c.Len())
	c.Evict(a)
	_, ok := c.get(a)
	assert.False(t, ok)
	_, ok = c.get(b)
	assert.True(t, ok, "only the named subject is evicted")

	fill()
	c.EvictRole(10)
	_, ok = c.get(a)
	assert.False(t, ok)
	_, ok = c.get(d)
	assert.False(t, ok, "every holder of the role, whatever its kind")
	_, ok = c.get(b)
	assert.True(t, ok, "holders of other roles keep their entry")

	fill()
	c.EvictAll()
	assert.Zero(t, c.Len())
}

func TestCacheExpiresEntries(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewCache(time.Minute)
	c.now = func() time.Time { return now }
	s := Subject{KindUser, 1}
	c.put(s, effWithRole(1), c.begin())
	_, ok := c.get(s)
	assert.True(t, ok)
	now = now.Add(59 * time.Second)
	_, ok = c.get(s)
	assert.True(t, ok)
	now = now.Add(2 * time.Second)
	_, ok = c.get(s)
	assert.False(t, ok, "the TTL is the backstop for changes made by another process")
}

func TestCacheDiscardsLoadsThatRacedAnEviction(t *testing.T) {
	c := NewCache(time.Minute)
	s := Subject{KindUser, 1}

	epoch := c.begin() // a slow reader starts loading...
	c.Evict(s)         // ...a binding changes meanwhile...
	c.put(s, effWithRole(1), epoch)
	_, ok := c.get(s)
	assert.False(t, ok, "...so the load's possibly stale result is not stored")

	c.put(s, effWithRole(1), c.begin())
	_, ok = c.get(s)
	assert.True(t, ok)
}

func TestNewCacheDefaultsTheTTL(t *testing.T) {
	assert.Equal(t, DefaultCacheTTL, NewCache(0).ttl)
	assert.Equal(t, DefaultCacheTTL, NewCache(-time.Second).ttl)
}
