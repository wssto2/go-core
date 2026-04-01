package audit

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wssto2/go-core/apperr"
)

// AsyncRepository writes audit entries to an underlying Repository asynchronously.
// It buffers entries in a channel and has a Shutdown method to flush remaining items.
type AsyncRepository struct {
	underlying Repository
	ch         chan Entry
	wg         sync.WaitGroup
	closeOnce  sync.Once
	workers    int
	OnError    func(Entry, error) // called on failed write
	closed     uint32             // atomic flag: 0=open, 1=closed
}

// NewAsyncRepository creates an AsyncRepository with the given buffer size and
// number of concurrent drain goroutines. If workers < 1 it defaults to 1.
func NewAsyncRepository(underlying Repository, buffer int, workers int) *AsyncRepository {
	if buffer <= 0 {
		buffer = 100
	}
	if workers < 1 {
		workers = 1
	}
	a := &AsyncRepository{
		underlying: underlying,
		ch:         make(chan Entry, buffer),
		workers:    workers,
	}
	for i := 0; i < workers; i++ {
		a.wg.Add(1)
		go a.loop()
	}
	return a
}

func (a *AsyncRepository) loop() {
	defer a.wg.Done()
	for e := range a.ch {
		err := a.underlying.Write(context.Background(), e)
		if err != nil && a.OnError != nil {
			a.OnError(e, err)
		}
		a.wg.Done()
	}
}

// Write enqueues an audit entry for asynchronous persistence. Returns an
// AppError if the internal buffer is full.
func (a *AsyncRepository) Write(ctx context.Context, entry Entry) error {
	if atomic.LoadUint32(&a.closed) == 1 {
		return apperr.New(nil, "audit repository is shut down", apperr.CodeInternal)
	}
	// Account for this item before attempting to enqueue. If enqueue fails
	// we decrement the counter to keep the wait group balanced.
	a.wg.Add(1)
	select {
	case a.ch <- entry:
		return nil
	default:
		a.wg.Done()
		return apperr.New(nil, "audit queue full", apperr.CodeInternal)
	}
}

// Shutdown closes the queue and waits for pending items to be processed. If the
// provided context expires before flush completes an AppError is returned.
func (a *AsyncRepository) Shutdown(ctx context.Context) error {
	a.closeOnce.Do(func() {
		atomic.StoreUint32(&a.closed, 1)
		close(a.ch)
	})
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return apperr.Wrap(ctx.Err(), "shutdown timed out", apperr.CodeInternal)
	}
}
