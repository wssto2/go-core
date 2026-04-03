package ratelimit_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wssto2/go-core/ratelimit"
)

// --- In-memory limiter tests ---
func TestInMemoryLimiter_Basic(t *testing.T) {
	l := ratelimit.NewInMemoryLimiter(3, 150*time.Millisecond)
	key := "user:1"

	// allow 3 times
	for i := 0; i < 3; i++ {
		ok, err := l.Allow(context.Background(), key)
		require.NoError(t, err)
		require.True(t, ok)
	}

	// 4th should be denied
	ok, err := l.Allow(context.Background(), key)
	require.NoError(t, err)
	require.False(t, ok)

	// after window expiry it should allow again
	time.Sleep(200 * time.Millisecond)
	ok, err = l.Allow(context.Background(), key)
	require.NoError(t, err)
	require.True(t, ok)
}

// --- Fake Redis executor for tests ---
type fakeExec struct {
	mu      sync.Mutex
	counts  map[string]int64
	expires map[string]time.Time
}

func newFakeExec() *fakeExec {
	return &fakeExec{counts: make(map[string]int64), expires: make(map[string]time.Time)}
}

func (f *fakeExec) Eval(ctx context.Context, script string, keys []string, args ...interface{}) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := keys[0]
	// expire if needed
	if exp, ok := f.expires[k]; ok && time.Now().After(exp) {
		delete(f.counts, k)
		delete(f.expires, k)
	}
	v := f.counts[k] + 1
	f.counts[k] = v
	if _, ok := f.expires[k]; !ok {
		// args[0] expected to be ms (int64/int)
		var ms int64 = 1000
		if len(args) > 0 {
			switch a := args[0].(type) {
			case int64:
				ms = a
			case int:
				ms = int64(a)
			case float64:
				ms = int64(a)
			}
		}
		f.expires[k] = time.Now().Add(time.Duration(ms) * time.Millisecond)
	}
	return v, nil
}

func TestInMemoryLimiter_ExpiredEntries_AreEvicted(t *testing.T) {
window := 50 * time.Millisecond
limiter := ratelimit.NewInMemoryLimiter(10, window)
defer limiter.Stop()

ctx := context.Background()
for i := 0; i < 20; i++ {
_, _ = limiter.Allow(ctx, "ip"+string(rune('A'+i)))
}

before := limiter.Len()
if before == 0 {
t.Fatal("expected non-empty map before sweep")
}

time.Sleep(window * 4)

after := limiter.Len()
if after != 0 {
t.Fatalf("expected empty map after sweep, got %d entries", after)
}

// Stop must be idempotent.
limiter.Stop()
limiter.Stop()
}
