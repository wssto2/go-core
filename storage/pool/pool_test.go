package pool

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/wssto2/go-core/apperr"
)

// fakeConn is a tiny Conn implementation for tests.
type fakeConn struct {
	id     int
	mu     sync.Mutex
	closed bool
}

func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return nil
}

func TestChannelPool_ReuseConn(t *testing.T) {
	var id int
	var mu sync.Mutex
	factory := func(ctx context.Context) (io.Closer, error) {
		mu.Lock()
		id++
		n := id
		mu.Unlock()
		return &fakeConn{id: n}, nil
	}

	p, err := NewChannelPool(factory, 1)
	if err != nil {
		t.Fatalf("NewChannelPool: %v", err)
	}
	ctx := context.Background()

	c1, err := p.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if c1 == nil {
		t.Fatalf("nil conn")
	}

	if err := p.Put(ctx, c1); err != nil {
		t.Fatalf("Put: %v", err)
	}

	c2, err := p.Get(ctx)
	if err != nil {
		t.Fatalf("Get 2: %v", err)
	}
	if c1 != c2 {
		t.Fatalf("expected same conn, got %v %v", c1, c2)
	}
	_ = p.Put(ctx, c2)
	_ = p.Close(ctx)
}

func TestChannelPool_Exhaustion(t *testing.T) {
	factory := func(ctx context.Context) (io.Closer, error) {
		return &fakeConn{}, nil
	}
	p, err := NewChannelPool(factory, 1)
	if err != nil {
		t.Fatalf("NewChannelPool: %v", err)
	}
	ctx := context.Background()

	c1, err := p.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// do not return c1 yet

	ctx2, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = p.Get(ctx2)
	if err == nil {
		t.Fatalf("expected error due to exhaustion")
	}
	var aerr *apperr.AppError
	if !errors.As(err, &aerr) {
		t.Fatalf("expected apperr.AppError, got %T", err)
	}
	if !errors.Is(aerr.Err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded wrapped, got %v", aerr.Err)
	}

	// cleanup
	_ = p.Put(ctx, c1)
	_ = p.Close(ctx)
}

func TestChannelPool_Close(t *testing.T) {
	var id int
	var mu sync.Mutex
	factory := func(ctx context.Context) (io.Closer, error) {
		mu.Lock()
		id++
		n := id
		mu.Unlock()
		return &fakeConn{id: n}, nil
	}
	p, err := NewChannelPool(factory, 2)
	if err != nil {
		t.Fatalf("NewChannelPool: %v", err)
	}
	ctx := context.Background()

	c1, err := p.Get(ctx)
	if err != nil {
		t.Fatalf("Get1: %v", err)
	}
	c2, err := p.Get(ctx)
	if err != nil {
		t.Fatalf("Get2: %v", err)
	}
	_ = p.Put(ctx, c1)
	_ = p.Put(ctx, c2)

	_ = p.Close(ctx)

	_, err = p.Get(context.Background())
	if err == nil {
		t.Fatalf("expected error after close")
	}
	var aerr *apperr.AppError
	if !errors.As(err, &aerr) {
		t.Fatalf("expected apperr.AppError, got %T", err)
	}
	if aerr.Code != apperr.CodeInternal {
		t.Fatalf("expected CodeInternal, got %s", aerr.Code)
	}

	f1, ok1 := c1.(*fakeConn)
	f2, ok2 := c2.(*fakeConn)
	if !ok1 || !ok2 {
		t.Fatalf("expected fakeConn types")
	}
	if !f1.closed || !f2.closed {
		t.Fatalf("expected closed conns after pool.Close")
	}
}

func TestChannelPool_Put_AfterClose_NoRace(t *testing.T) {
	factory := func(ctx context.Context) (io.Closer, error) {
		return &fakeConn{}, nil
	}
	p, err := NewChannelPool(factory, 2)
	if err != nil {
		t.Fatalf("NewChannelPool: %v", err)
	}
	ctx := context.Background()

	c1, err := p.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if err := p.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Put after close must return an error and not deadlock or race.
	putErr := p.Put(ctx, c1)
	if putErr == nil {
		t.Fatal("expected error from Put after Close, got nil")
	}

	// A second Put with a fresh connection must also return an error.
	putErr2 := p.Put(ctx, &fakeConn{})
	if putErr2 == nil {
		t.Fatal("expected error from second Put after Close, got nil")
	}
}
