package resilience

import (
	"context"
	"time"
)

// WithTimeout runs op with a child context that has the provided timeout.
// If timeout <= 0 the operation is invoked directly with the original context.
// The function returns the operation error, or a context error if the parent
// context is canceled or the timeout elapses before op returns.
//
// Note: if op ignores context cancellation it may continue running in the
// background; WithTimeout returns early when the timeout elapses.
func WithTimeout(ctx context.Context, timeout time.Duration, op func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return op(ctx)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- op(ctx2)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-ctx2.Done():
		return ctx2.Err()
	}
}
