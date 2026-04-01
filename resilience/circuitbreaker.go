package resilience

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrOpen is returned when the circuit breaker is open and calls are rejected.
var ErrOpen = errors.New("circuit breaker open")

// State represents the circuit breaker state.
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// CircuitBreaker is a minimal, thread-safe circuit breaker.
// - CLOSED: calls are executed; failures are counted
// - OPEN: calls are rejected until openTimeout elapses
// - HALF-OPEN: a single trial call is allowed; success -> CLOSED, failure -> OPEN
type CircuitBreaker struct {
	mu sync.RWMutex
	// runtime state
	state           State
	failures        int
	openedAt        time.Time
	trialInProgress bool

	// configuration
	failureThreshold int
	openTimeout      time.Duration
}

// NewCircuitBreaker creates a new CircuitBreaker. failureThreshold must be >= 1.
// openTimeout controls how long the breaker stays OPEN before transitioning to HALF-OPEN.
func NewCircuitBreaker(failureThreshold int, openTimeout time.Duration) *CircuitBreaker {
	if failureThreshold < 1 {
		failureThreshold = 1
	}
	if openTimeout <= 0 {
		openTimeout = time.Second
	}
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		openTimeout:      openTimeout,
	}
}

// State returns the current breaker state.
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Execute runs the provided operation respecting the circuit state.
// If the breaker is OPEN and the open timeout has not elapsed, ErrOpen is returned.
// When transitioning to HALF-OPEN, only a single trial is allowed; concurrent callers
// will receive ErrOpen until the trial finishes.
// runTrial encapsulates the logic for executing a trial call in HALF-OPEN state.
func (cb *CircuitBreaker) runTrial(ctx context.Context, op func(context.Context) error) error {
	err := op(ctx)

	cb.mu.Lock()
	cb.trialInProgress = false
	if err == nil {
		cb.state = StateClosed
		cb.failures = 0
	} else {
		cb.state = StateOpen
		cb.openedAt = time.Now()
		cb.failures = 0
	}
	cb.mu.Unlock()
	return err
}

func (cb *CircuitBreaker) Execute(ctx context.Context, op func(context.Context) error) error {

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Fast path: inspect state while holding lock and decide behavior.
	cb.mu.Lock()
	now := time.Now()
	switch cb.state {
	case StateOpen:
		// still open?
		if now.Sub(cb.openedAt) < cb.openTimeout {
			cb.mu.Unlock()
			return ErrOpen
		}
		// timeout elapsed -> allow a single trial in HALF-OPEN
		cb.state = StateHalfOpen
		cb.trialInProgress = true
		cb.mu.Unlock()

		return cb.runTrial(ctx, op)

	case StateHalfOpen:
		// if a trial is already in progress, reject
		if cb.trialInProgress {
			cb.mu.Unlock()
			return ErrOpen
		}
		cb.trialInProgress = true
		cb.mu.Unlock()

		return cb.runTrial(ctx, op)

	case StateClosed:
		cb.mu.Unlock()

		// normal execution
		err := op(ctx)

		cb.mu.Lock()
		if err == nil {
			cb.failures = 0
		} else {
			cb.failures++
			if cb.failures >= cb.failureThreshold {
				cb.state = StateOpen
				cb.openedAt = time.Now()
			}
		}
		cb.mu.Unlock()
		return err

	default:
		cb.mu.Unlock()
		return ErrOpen
	}
}
