package resilience

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetry_SuccessFirstTry(t *testing.T) {
	ctx := context.Background()
	var calls int32
	err := Retry(ctx, 3, 10*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestRetry_EventualSuccess(t *testing.T) {
	ctx := context.Background()
	var calls int32
	err := Retry(ctx, 5, 5*time.Millisecond, func(ctx context.Context) error {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return errors.New("temporary")
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

func TestRetry_ExhaustedAttempts(t *testing.T) {
	ctx := context.Background()
	var calls int32
	sentinel := errors.New("fail")
	err := Retry(ctx, 3, 1*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return sentinel
	})
	assert.Error(t, err)
	assert.Equal(t, sentinel, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

func TestRetry_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var calls int32
	start := time.Now()
	err := Retry(ctx, 10, 20*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("fail")
	})
	elapsed := time.Since(start)
	assert.Error(t, err)
	// expect context cancellation (deadline exceeded or canceled)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
	// ensure it returned reasonably quickly (not waiting for all attempts)
	assert.True(t, elapsed < 500*time.Millisecond)
	assert.True(t, atomic.LoadInt32(&calls) >= 1)
}

func TestRetry_InvalidAttempts(t *testing.T) {
	ctx := context.Background()
	err := Retry(ctx, 0, 10*time.Millisecond, func(ctx context.Context) error { return nil })
	assert.Error(t, err)
}
