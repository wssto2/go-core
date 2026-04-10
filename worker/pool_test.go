package worker

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPool_SubmitRejectsWhenFullBeforeStart(t *testing.T) {
	p := New(WithWorkers(1), WithQueueSize(2), WithLogger(slog.Default()))
	ctx := context.Background()

	err := p.Submit(ctx, func(ctx context.Context) error { return nil })
	assert.NoError(t, err)
	err = p.Submit(ctx, func(ctx context.Context) error { return nil })
	assert.NoError(t, err)
	err = p.Submit(ctx, func(ctx context.Context) error { return nil })
	assert.Equal(t, ErrQueueFull, err)
}

func TestPool_BoundedWorkersNoUnboundedGoroutines(t *testing.T) {
	p := New(WithWorkers(2), WithQueueSize(100), WithLogger(slog.Default()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := atomic.Int32{}
	done := make(chan struct{})

	p.Start(ctx)

	// Submit 10 blocking jobs. Only `workers` should run concurrently.
	for i := 0; i < 10; i++ {
		err := p.Submit(context.Background(), func(ctx context.Context) error {
			started.Add(1)
			<-done
			return nil
		})
		assert.NoError(t, err)
	}

	// Wait until exactly 2 workers are running.
	assert.Eventually(t, func() bool { return started.Load() == 2 }, 2*time.Second, 10*time.Millisecond)

	close(done)
	cancel()
	p.Wait()
}

func TestPool_RejectsWhenFullWhenRunning(t *testing.T) {
	p := New(WithWorkers(1), WithQueueSize(1), WithLogger(slog.Default()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	done := make(chan struct{})

	err := p.Submit(context.Background(), func(ctx context.Context) error { <-done; return nil })
	assert.NoError(t, err)

	// give the worker a moment to pick up the job
	time.Sleep(20 * time.Millisecond)

	// second should fill queue
	err = p.Submit(context.Background(), func(ctx context.Context) error { return nil })
	assert.NoError(t, err)

	// third should be rejected
	err = p.Submit(context.Background(), func(ctx context.Context) error { return nil })
	assert.Equal(t, ErrQueueFull, err)

	close(done)
	cancel()
	p.Wait()
}
