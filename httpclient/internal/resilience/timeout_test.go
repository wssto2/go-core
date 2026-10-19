package resilience

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWithTimeout_SucceedsBeforeTimeout(t *testing.T) {
	ctx := context.Background()
	start := time.Now()
	err := WithTimeout(ctx, 100*time.Millisecond, func(ctx context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	elapsed := time.Since(start)
	assert.NoError(t, err)
	assert.True(t, elapsed < 100*time.Millisecond)
}

func TestWithTimeout_TimesOutWhenOpRespectsCtx(t *testing.T) {
	ctx := context.Background()
	err := WithTimeout(ctx, 30*time.Millisecond, func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return nil
		}
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestWithTimeout_ReturnsEarlyIfOpIgnoresCtx(t *testing.T) {
	ctx := context.Background()
	start := time.Now()
	err := WithTimeout(ctx, 30*time.Millisecond, func(ctx context.Context) error {
		time.Sleep(200 * time.Millisecond) // ignores ctx
		return nil
	})
	elapsed := time.Since(start)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	// ensure wrapper returned early (didn't wait for 200ms)
	assert.True(t, elapsed < 150*time.Millisecond)
}

func TestWithTimeout_ParentCanceled(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	err := WithTimeout(parent, 50*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt32(new(int32), 1)
		return nil
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestWithTimeout_ZeroTimeoutCallsDirectly(t *testing.T) {
	ctx := context.Background()
	var called int32
	err := WithTimeout(ctx, 0, func(ctx context.Context) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&called))
}
