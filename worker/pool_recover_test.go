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

func TestPool_RecoverFromPanic(t *testing.T) {
	t.Parallel()

	pool := NewPool(1, 10, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Start(ctx)

	// submit job that panics
	err := pool.Submit(context.Background(), func(_ context.Context) error {
		panic("crash")
	})
	require.NoError(t, err)

	// submit job that should run after panic
	ran := atomic.Int32{}
	err = pool.Submit(context.Background(), func(_ context.Context) error {
		ran.Add(1)
		return nil
	})
	require.NoError(t, err)

	// wait for job to execute
	assert.Eventually(t, func() bool { return ran.Load() == 1 }, time.Second, 10*time.Millisecond)

	cancel()
	pool.Wait()
}
