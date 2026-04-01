package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockWorker struct {
	name    string
	runFunc func(ctx context.Context) error
	runs    atomic.Int32
}

func (m *mockWorker) Name() string { return m.name }
func (m *mockWorker) Run(ctx context.Context) error {
	m.runs.Add(1)
	return m.runFunc(ctx)
}

func TestManager_PanicRecovery(t *testing.T) {
	m := NewManager(slog.Default())

	var worker *mockWorker
	worker = &mockWorker{
		name: "panic_worker",
		runFunc: func(ctx context.Context) error {
			if worker.runs.Load() == 1 {
				panic("oops")
			}
			<-ctx.Done()
			return nil
		},
	}

	m.Add(worker)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	m.Start(ctx)

	// Wait a bit for the restart
	time.Sleep(1500 * time.Millisecond)
	cancel()
	m.Wait()

	assert.GreaterOrEqual(t, worker.runs.Load(), int32(2), "Worker should have restarted after panic")
}

func TestManager_RestartOnError(t *testing.T) {
	m := NewManager(slog.Default())

	var worker *mockWorker
	worker = &mockWorker{
		name: "error_worker",
		runFunc: func(ctx context.Context) error {
			if worker.runs.Load() == 1 {
				return errors.New("error")
			}
			<-ctx.Done()
			return nil
		},
	}

	m.Add(worker)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	m.Start(ctx)

	// Wait a bit for the restart
	time.Sleep(1500 * time.Millisecond)
	cancel()
	m.Wait()

	assert.GreaterOrEqual(t, worker.runs.Load(), int32(2), "Worker should have restarted after error")
}
