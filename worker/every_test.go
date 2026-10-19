//nolint:paralleltest // tests in the style of the package's others
package worker_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/worker"
)

func TestEvery_RunsFirstAtOnceThenEveryInterval_ErrorsAreNotFatal(t *testing.T) {
	var calls atomic.Int32

	ticked := make(chan struct{}, 8)

	every := worker.Every("reminders", 5*time.Millisecond, func(context.Context) error {
		ticked <- struct{}{}

		if calls.Add(1) == 1 {
			return errors.New("boom") //nolint:err113 // a test error
		}

		return nil
	})
	assert.Equal(t, "reminders", every.Name())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- every.Run(ctx) }()

	for range 3 {
		select {
		case <-ticked:
		case <-time.After(2 * time.Second):
			t.Fatal("fn was not called again after an error")
		}
	}

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the context ended")
	}

	settled := calls.Load()

	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, settled, calls.Load(), "fn runs no more after Run returned")
}
