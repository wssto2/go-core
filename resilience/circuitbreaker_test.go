package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCircuitBreaker_TripsOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 200*time.Millisecond)
	ctx := context.Background()
	var calls int32
	op := func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("fail")
	}

	// two failing calls should trip the breaker
	err := cb.Execute(ctx, op)
	assert.Error(t, err)
	err = cb.Execute(ctx, op)
	assert.Error(t, err)

	// next call should be rejected immediately (breaker open)
	err = cb.Execute(ctx, op)
	assert.Equal(t, ErrOpen, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestCircuitBreaker_AllowsAfterTimeoutAndRecovers(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)
	ctx := context.Background()

	// trip it
	err := cb.Execute(ctx, func(ctx context.Context) error { return errors.New("fail") })
	assert.Error(t, err)

	// immediate attempt rejected
	err = cb.Execute(ctx, func(ctx context.Context) error { return nil })
	assert.Equal(t, ErrOpen, err)

	// wait for timeout -> half-open trial allowed
	time.Sleep(60 * time.Millisecond)
	err = cb.Execute(ctx, func(ctx context.Context) error { return nil })
	assert.NoError(t, err)

	// after successful trial it should be closed
	err = cb.Execute(ctx, func(ctx context.Context) error { return nil })
	assert.NoError(t, err)
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)
	ctx := context.Background()

	// trip it
	err := cb.Execute(ctx, func(ctx context.Context) error { return errors.New("fail") })
	assert.Error(t, err)

	// wait for timeout -> half-open trial allowed
	time.Sleep(60 * time.Millisecond)
	err = cb.Execute(ctx, func(ctx context.Context) error { return errors.New("still fail") })
	assert.Error(t, err)

	// immediately should be open again
	err = cb.Execute(ctx, func(ctx context.Context) error { return nil })
	assert.Equal(t, ErrOpen, err)
}

func TestCircuitBreaker_ConcurrentHalfOpenSingleTrial(t *testing.T) {
	cb := NewCircuitBreaker(1, 20*time.Millisecond)
	ctx := context.Background()

	// trip it
	_ = cb.Execute(ctx, func(ctx context.Context) error { return errors.New("fail") })
	// wait for open timeout
	time.Sleep(30 * time.Millisecond)

	var wg sync.WaitGroup
	var success int32
	calls := int32(0)
	workers := 6
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := cb.Execute(ctx, func(ctx context.Context) error {
				atomic.AddInt32(&calls, 1)
				// simulate work so other callers overlap
				time.Sleep(40 * time.Millisecond)
				return nil
			})
			if err == nil {
				atomic.AddInt32(&success, 1)
			} else {
				assert.Equal(t, ErrOpen, err)
			}
		}()
	}
	wg.Wait()
	// Only one goroutine should have been allowed to run the trial
	assert.Equal(t, int32(1), atomic.LoadInt32(&success))
	assert.True(t, atomic.LoadInt32(&calls) >= 1)
}
