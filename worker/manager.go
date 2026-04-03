package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
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
	workers      []Worker
	logger       *slog.Logger
	metrics      *ManagerMetrics
	wg           sync.WaitGroup
	mu           sync.Mutex
	running      bool
	maxRestarts  int           // 0 = unlimited
	initialDelay time.Duration // base for backoff; defaults to 1s
	successAfter time.Duration // running longer than this resets restart count; defaults to 30s
}

func NewManager(logger *slog.Logger, opts ...ManagerOption) *Manager {
	m := &Manager{
		logger:       logger,
		initialDelay: time.Second,
		successAfter: 30 * time.Second,
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

type ManagerOption func(*Manager)

func WithManagerMetrics(metrics *ManagerMetrics) ManagerOption {
	return func(m *Manager) { m.metrics = metrics }
}

// WithMaxRestarts sets the maximum number of consecutive failures before
// the worker is permanently stopped. 0 means unlimited (default).
func WithMaxRestarts(n int) ManagerOption {
	return func(m *Manager) { m.maxRestarts = n }
}

// WithInitialDelay overrides the base backoff delay (default 1s).
func WithInitialDelay(d time.Duration) ManagerOption {
	return func(m *Manager) { m.initialDelay = d }
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

	restarts := 0

	for {
		start := time.Now()
		err := m.safeRun(ctx, w)

		// If context is cancelled, normal shutdown
		if ctx.Err() != nil {
			m.logger.Info("worker_stopped", "worker", w.Name())
			return
		}

		if time.Since(start) > m.successAfter {
			// Worker ran long enough to be considered healthy — reset backoff.
			restarts = 0
		}

		if err != nil {
			m.logger.Error("worker_failed", "worker", w.Name(), "error", err, "restarts", restarts)
			if m.metrics != nil {
				if isPanic(err) {
					m.metrics.workerPanics.WithLabelValues(w.Name()).Inc()
				} else {
					m.metrics.workerErrors.WithLabelValues(w.Name()).Inc()
				}
			}
		}

		restarts++

		if m.maxRestarts > 0 && restarts >= m.maxRestarts {
			m.logger.Error("worker_max_restarts_reached", "worker", w.Name(), "restarts", restarts)
			return
		}

		if m.metrics != nil {
			m.metrics.workerRestarts.WithLabelValues(w.Name()).Inc()
		}

		delay := m.backoffDelay(restarts)
		m.logger.Info("worker_restarting", "worker", w.Name(), "delay", delay, "restarts", restarts)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// backoffDelay computes min(initialDelay * 2^(restarts-1), 60s) with ±25% jitter.
func (m *Manager) backoffDelay(restarts int) time.Duration {
	exp := restarts - 1
	if exp < 0 {
		exp = 0
	}
	factor := math.Pow(2, float64(exp))
	base := float64(m.initialDelay) * factor
	const maxDelay = float64(60 * time.Second)
	if base > maxDelay {
		base = maxDelay
	}
	// ±25% jitter
	jitter := base * 0.25 * (2*rand.Float64() - 1)
	result := time.Duration(base + jitter)
	if result < 0 {
		result = 0
	}
	return result
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
