package audit

import (
	"context"
	"sync"

	"github.com/wssto2/go-core/apperr"
)

// AsyncRepository writes audit entries to an underlying Repository asynchronously.
// It buffers entries in a channel and has a Shutdown method to flush remaining items.
type AsyncRepository struct {
	underlying Repository
	ch         chan Entry
	wg         sync.WaitGroup
	closeOnce  sync.Once
	mu         sync.Mutex // guards closed, wg.Add, and ch send atomically
	closed     bool
	workers    int
	OnError    func(Entry, error) // called on failed write
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
// AppError if the repository is shut down or the internal buffer is full.
//
// The mutex is held across the closed-check, wg.Add(1), and the non-blocking
// channel send so that Shutdown cannot close the channel between any of those
// three steps (which would cause a WaitGroup panic or a send-on-closed panic).
func (a *AsyncRepository) Write(ctx context.Context, entry Entry) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return apperr.New(nil, "audit repository is shut down", apperr.CodeInternal)
	}
	a.wg.Add(1)
	select {
	case a.ch <- entry:
		a.mu.Unlock()
		return nil
	default:
		a.wg.Done()
		a.mu.Unlock()
		return apperr.New(nil, "audit queue full", apperr.CodeInternal)
	}
}

// Shutdown closes the queue and waits for pending items to be processed. If the
// provided context expires before flush completes an AppError is returned.
//
// closed is set under mu (same mutex Write uses) so that no new wg.Add(1) calls
// can race with wg.Wait().
func (a *AsyncRepository) Shutdown(ctx context.Context) error {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		close(a.ch)
		a.mu.Unlock()
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
