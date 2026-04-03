package pool

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/wssto2/go-core/apperr"
)

// NewConnFunc creates a new connection instance. For SFTP/FTP this would dial the server.
type NewConnFunc func(ctx context.Context) (io.Closer, error)

// Pool is a minimal connection pool interface.
type Pool interface {
	Get(ctx context.Context) (io.Closer, error)
	Put(ctx context.Context, c io.Closer) error
	Close(ctx context.Context) error
}

// channelPool is a simple, bounded pool implemented with a buffered channel.
// It creates connections lazily up to capacity and reuses idle connections.
type channelPool struct {
	factory  NewConnFunc
	conns    chan io.Closer
	capacity int

	mu     sync.Mutex
	total  int
	closed bool
}

// NewChannelPool constructs a new pool with the given capacity.
func NewChannelPool(factory NewConnFunc, capacity int) (Pool, error) {
	if factory == nil {
		return nil, apperr.BadRequest("factory is nil")
	}
	if capacity <= 0 {
		return nil, apperr.BadRequest("capacity must be > 0")
	}
	p := &channelPool{
		factory:  factory,
		conns:    make(chan io.Closer, capacity),
		capacity: capacity,
	}
	return p, nil
}

// Get acquires a connection from the pool, creating a new one if under capacity.
// If capacity is reached it waits for an available connection or context cancellation.
func (p *channelPool) Get(ctx context.Context) (io.Closer, error) {
	// Fast-path: try to get an idle connection without blocking.
	select {
	case c, ok := <-p.conns:
		if !ok {
			return nil, apperr.Internal(fmt.Errorf("pool closed"))
		}
		return c, nil
	default:
	}

	// Otherwise try to create a new connection if we haven't hit capacity.
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, apperr.Internal(fmt.Errorf("pool closed"))
	}
	if p.total < p.capacity {
		p.total++
		p.mu.Unlock()
		c, err := p.factory(ctx)
		if err != nil {
			// creation failed, decrement total and return wrapped error
			p.mu.Lock()
			p.total--
			p.mu.Unlock()
			return nil, apperr.Internal(err)
		}
		return c, nil
	}
	p.mu.Unlock()

	// Wait for an available connection or context cancellation.
	select {
	case c, ok := <-p.conns:
		if !ok {
			return nil, apperr.Internal(fmt.Errorf("pool closed"))
		}
		return c, nil
	case <-ctx.Done():
		return nil, apperr.Internal(ctx.Err())
	}
}

// Put returns a connection to the pool. If the pool is closed or full the connection
// is closed and discarded.
//
// The mutex is held across the closed-check and the non-blocking channel send so
// that a concurrent Close() cannot close the channel between those two operations
// (which would cause a "send on closed channel" panic). The send is always
// non-blocking (select/default), so holding the mutex here cannot deadlock.
func (p *channelPool) Put(ctx context.Context, c io.Closer) error {
	p.mu.Lock()
	if p.closed {
		p.total--
		if p.total < 0 {
			p.total = 0
		}
		p.mu.Unlock()
		if c != nil {
			_ = c.Close()
		}
		return apperr.Internal(fmt.Errorf("pool closed"))
	}

	select {
	case p.conns <- c:
		// returned to the pool
		p.mu.Unlock()
		return nil
	default:
		// channel full: close and discard
		p.total--
		if p.total < 0 {
			p.total = 0
		}
		p.mu.Unlock()
		if c != nil {
			_ = c.Close()
		}
		return nil
	}
}

// Close shuts down the pool and closes all idle connections.
// Borrowed connections will be closed when Put is called.
func (p *channelPool) Close(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.conns)
	p.mu.Unlock()

	// drain and close idle connections
	closed := 0
	for c := range p.conns {
		if c != nil {
			_ = c.Close()
			closed++
		}
	}

	p.mu.Lock()
	p.total -= closed
	if p.total < 0 {
		p.total = 0
	}
	p.mu.Unlock()
	return nil
}
