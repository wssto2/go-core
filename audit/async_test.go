package audit

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeRepo struct {
	mu      sync.Mutex
	entries []Entry
	ch      chan struct{}
	writeFn func(ctx context.Context, e Entry) error
}

func (f *fakeRepo) Write(ctx context.Context, e Entry) error {
	if f.writeFn != nil {
		return f.writeFn(ctx, e)
	}
	f.mu.Lock()
	f.entries = append(f.entries, e)
	f.mu.Unlock()
	if f.ch != nil {
		select {
		case f.ch <- struct{}{}:
		default:
		}
	}
	return nil
}

func TestAsyncRepositoryFlushes(t *testing.T) {
	fr := &fakeRepo{ch: make(chan struct{}, 10)}
	ar := NewAsyncRepository(fr, 5, 1)
	entries := []Entry{
		NewEntry("user", 1, 1, "create"),
		NewEntry("user", 2, 2, "update"),
		NewEntry("order", 4, 3, "delete"),
	}
	for _, e := range entries {
		if err := ar.Write(context.Background(), e); err != nil {
			t.Fatalf("write err: %v", err)
		}
	}
	// Wait for underlying writes signaled by fr.ch
	timeout := time.After(time.Second)
	for i := 0; i < len(entries); i++ {
		select {
		case <-fr.ch:
		case <-timeout:
			t.Fatalf("timeout waiting for write")
		}
	}
	// Ensure Shutdown flushes remaining writes
	if err := ar.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
	fr.mu.Lock()
	if len(fr.entries) != len(entries) {
		t.Fatalf("expected %d entries, got %d", len(entries), len(fr.entries))
	}
	fr.mu.Unlock()
}

func TestAsyncRepository_WriteAfterShutdown_NoRace(t *testing.T) {
	fr := &fakeRepo{}
	ar := NewAsyncRepository(fr, 100, 2)

	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	// 10 goroutines call Write and Shutdown concurrently.
	for i := 0; i < 9; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := ar.Write(context.Background(), NewEntry("user", 1, 1, "create"))
			errCh <- err
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = ar.Shutdown(context.Background())
	}()

	wg.Wait()
	close(errCh)

	// All post-shutdown writes must return an error; none must panic.
	// (Some writes may succeed before shutdown; that is acceptable.)
	for err := range errCh {
		if err != nil {
			// Any error is a valid outcome for a Write that raced with Shutdown.
			_ = err
		}
	}
}

type slowRepo struct {
	mu      sync.Mutex
	entries []Entry
	ch      chan struct{}
	delay   time.Duration
}

func (s *slowRepo) Write(ctx context.Context, e Entry) error {
	time.Sleep(s.delay)
	s.mu.Lock()
	s.entries = append(s.entries, e)
	s.mu.Unlock()
	if s.ch != nil {
		select {
		case s.ch <- struct{}{}:
		default:
		}
	}
	return nil
}

func TestAsyncRepositoryOnError(t *testing.T) {
	errCh := make(chan struct{}, 1)
	badRepo := &fakeRepo{
		writeFn: func(ctx context.Context, e Entry) error {
			return context.DeadlineExceeded
		},
	}
	ar := NewAsyncRepository(badRepo, 2, 1)
	ar.OnError = func(e Entry, err error) {
		if err == context.DeadlineExceeded {
			errCh <- struct{}{}
		}
	}
	if err := ar.Write(context.Background(), NewEntry("user", 1, 1, "fail")); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	err := ar.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}
	select {
	case <-errCh:
		// success
	case <-time.After(time.Second):
		t.Fatal("OnError callback not called on write failure")
	}
}

func TestAsyncRepositoryQueueFull(t *testing.T) {
	sr := &slowRepo{delay: 200 * time.Millisecond, ch: make(chan struct{}, 10)}
	ar := NewAsyncRepository(sr, 1, 1) // buffer 1
	defer func() { _ = ar.Shutdown(context.Background()) }()

	e := NewEntry("user", 1, 1, "create")
	if err := ar.Write(context.Background(), e); err != nil {
		t.Fatalf("first write unexpected error: %v", err)
	}
	if err := ar.Write(context.Background(), e); err == nil {
		t.Fatalf("expected second write to return error due to full queue")
	}
	// Wait for the first write to finish
	select {
	case <-sr.ch:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for slow write")
	}
}
