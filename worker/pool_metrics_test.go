package worker

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testMetrics struct {
	submitted atomic.Int32
	rejected  atomic.Int32
	panicked  atomic.Int32
}

func (m *testMetrics) TasksSubmitted(ctx context.Context) { m.submitted.Add(1) }
func (m *testMetrics) TasksRejected(ctx context.Context)  { m.rejected.Add(1) }
func (m *testMetrics) TasksPanicked(ctx context.Context)  { m.panicked.Add(1) }

func TestPool_Metrics_SubmitReject(t *testing.T) {
	m := &testMetrics{}
	p := New(WithWorkers(1), WithQueueSize(1), WithLogger(slog.Default()), WithMetrics(m))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	// Submit first blocking job
	done := make(chan struct{})
	err := p.Submit(context.Background(), func(ctx context.Context) error {
		<-done
		return nil
	})
	require.NoError(t, err)

	// give worker time to pick up the first job
	time.Sleep(20 * time.Millisecond)

	// Second should be enqueued
	err = p.Submit(context.Background(), func(ctx context.Context) error { return nil })
	require.NoError(t, err)

	// Third should be rejected
	err = p.Submit(context.Background(), func(ctx context.Context) error { return nil })
	require.Equal(t, ErrQueueFull, err)

	// Metrics should reflect 2 submitted and 1 rejected
	assert.Equal(t, int32(2), m.submitted.Load())
	assert.Equal(t, int32(1), m.rejected.Load())

	close(done)
	cancel()
	p.Wait()
}

func TestPool_Metrics_Panic(t *testing.T) {
	m := &testMetrics{}
	p := New(WithWorkers(1), WithQueueSize(10), WithLogger(slog.Default()), WithMetrics(m))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	// submit job that panics
	err := p.Submit(context.Background(), func(ctx context.Context) error {
		panic("boom")
	})
	require.NoError(t, err)

	// wait for worker to process
	assert.Eventually(t, func() bool { return m.panicked.Load() == 1 }, 1*time.Second, 10*time.Millisecond)

	// ensure pool still accepts new job
	err = p.Submit(context.Background(), func(ctx context.Context) error { return nil })
	require.NoError(t, err)
	// Wait for job to be consumed
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(2), m.submitted.Load()) // panic job + this one
	cancel()
	p.Wait()
}
