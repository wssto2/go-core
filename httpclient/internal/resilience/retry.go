package resilience

import (
	"context"
	"errors"
	"math"
	"time"
)

// ErrInvalidAttempts is returned when attempts < 1
var ErrInvalidAttempts = errors.New("attempts must be >= 1")

// Retry runs the provided operation up to `attempts` times using exponential
// backoff starting from `initial`. The operation receives the context as its
// first argument and should respect it. If the context is canceled, Retry
// returns ctx.Err(). If attempts is exhausted, the last operation error is
// returned.
func Retry(ctx context.Context, attempts int, initial time.Duration, op func(context.Context) error) error {
	if attempts < 1 {
		return ErrInvalidAttempts
	}
	if initial <= 0 {
		initial = 100 * time.Millisecond
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		lastErr = op(ctx)
		if lastErr == nil {
			return nil
		}
		// if this was the last attempt, break and return the error
		if i == attempts-1 {
			break
		}

		// exponential backoff: initial * 2^i, capped to prevent overflow after many attempts
		const maxBackoff = 30 * time.Second
		backoff := time.Duration(float64(initial) * math.Pow(2, float64(i)))
		if backoff > maxBackoff || backoff < 0 {
			backoff = maxBackoff
		}
		// wait for backoff or context cancellation
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}
