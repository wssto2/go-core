package ratelimit_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wssto2/go-core/ratelimit"
)

func TestInMemoryLimiter_ZeroLimit_ClampedToOne(t *testing.T) {
	l := ratelimit.NewInMemoryLimiter(0, time.Second)
	defer l.Stop()

	ok, err := l.Allow(context.Background(), "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected first request to be allowed (limit clamped to 1)")
	}
	ok2, _ := l.Allow(context.Background(), "k")
	if ok2 {
		t.Fatal("expected second request to be denied (limit=1)")
	}
}

func TestInMemoryLimiter_ZeroWindow_ClampedToSecond(t *testing.T) {
	l := ratelimit.NewInMemoryLimiter(1, 0)
	defer l.Stop()
	// just verify it constructs and allows at least one request
	ok, err := l.Allow(context.Background(), "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected first request allowed")
	}
}

func TestInMemoryLimiter_DifferentKeys_IndependentWindows(t *testing.T) {
	l := ratelimit.NewInMemoryLimiter(2, time.Second)
	defer l.Stop()

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		ok, _ := l.Allow(ctx, "user:A")
		if !ok {
			t.Fatalf("user:A request %d should be allowed", i+1)
		}
	}
	// user:A exhausted
	if ok, _ := l.Allow(ctx, "user:A"); ok {
		t.Fatal("user:A 3rd request should be denied")
	}
	// user:B still has its own window
	if ok, _ := l.Allow(ctx, "user:B"); !ok {
		t.Fatal("user:B 1st request should be allowed independently")
	}
}

func TestInMemoryLimiter_WindowReset_AllowsAgain(t *testing.T) {
	window := 60 * time.Millisecond
	l := ratelimit.NewInMemoryLimiter(1, window)
	defer l.Stop()

	ctx := context.Background()
	ok, _ := l.Allow(ctx, "x")
	if !ok {
		t.Fatal("first request should be allowed")
	}
	ok2, _ := l.Allow(ctx, "x")
	if ok2 {
		t.Fatal("second request should be denied before window reset")
	}
	time.Sleep(window + 10*time.Millisecond)
	ok3, _ := l.Allow(ctx, "x")
	if !ok3 {
		t.Fatal("request after window expiry should be allowed")
	}
}

func TestInMemoryLimiter_ConcurrentAllows_NoRace(t *testing.T) {
	l := ratelimit.NewInMemoryLimiter(100, time.Second)
	defer l.Stop()

	var wg sync.WaitGroup
	const goroutines = 50
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = l.Allow(context.Background(), "shared-key")
		}()
	}
	wg.Wait()
}

func TestInMemoryLimiter_Len_TracksEntries(t *testing.T) {
	l := ratelimit.NewInMemoryLimiter(10, time.Second)
	defer l.Stop()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _ = l.Allow(ctx, "key-"+string(rune('A'+i)))
	}
	if n := l.Len(); n != 5 {
		t.Fatalf("expected 5 tracked keys, got %d", n)
	}
}

func TestInMemoryLimiter_Stop_Idempotent(t *testing.T) {
	l := ratelimit.NewInMemoryLimiter(5, 50*time.Millisecond)
	l.Stop()
	l.Stop() // must not panic
}
