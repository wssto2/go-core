package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// panicError wraps a recovered panic value for type-safe identification.
// It is returned by safeRun when a worker goroutine panics.
type panicError struct {
	value any
}

func (e *panicError) Error() string {
	return fmt.Sprintf("panic: %v", e.value)
}

// Manager orchestrates multiple workers, providing restart and panic recovery.
type Manager struct {
	workers []Worker
	logger  *slog.Logger
	metrics *ManagerMetrics
	wg      sync.WaitGroup
	mu      sync.Mutex
	running bool
}

func NewManager(logger *slog.Logger, opts ...ManagerOption) *Manager {
	m := &Manager{logger: logger}
	for _, o := range opts {
		o(m)
	}
	return m
}

type ManagerOption func(*Manager)

func WithManagerMetrics(metrics *ManagerMetrics) ManagerOption {
	return func(m *Manager) { m.metrics = metrics }
}

// Add appends one or more workers to the manager.
func (m *Manager) Add(workers ...Worker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workers = append(m.workers, workers...)
}

// Start launches all added workers in separate goroutines.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}
	m.running = true

	for _, w := range m.workers {
		m.wg.Add(1)
		go m.runWorker(ctx, w)
	}
}

func (m *Manager) runWorker(ctx context.Context, w Worker) {
	defer m.wg.Done()

	m.logger.Info("worker_starting", "worker", w.Name())

	for {
		err := m.safeRun(ctx, w)

		// If context is cancelled, normal shutdown
		if ctx.Err() != nil {
			m.logger.Info("worker_stopped", "worker", w.Name())
			return
		}

		if err != nil {
			m.logger.Error("worker_failed", "worker", w.Name(), "error", err)
			if m.metrics != nil {
				// distinguish panic from normal error via error message prefix
				if isPanic(err) {
					m.metrics.workerPanics.WithLabelValues(w.Name()).Inc()
				} else {
					m.metrics.workerErrors.WithLabelValues(w.Name()).Inc()
				}
			}
		}

		if m.metrics != nil {
			m.metrics.workerRestarts.WithLabelValues(w.Name()).Inc()
		}

		m.logger.Info("worker_restarting", "worker", w.Name())

		// Wait before restart to avoid tight loops on persistent errors
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
}

func (m *Manager) safeRun(ctx context.Context, w Worker) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &panicError{value: r}
		}
	}()

	return w.Run(ctx)
}

// Wait blocks until all workers have finished.
func (m *Manager) Wait() {
	m.wg.Wait()
}

func isPanic(err error) bool {
	var pe *panicError
	return errors.As(err, &pe)
}
