package worker

import (
	"context"
	"log/slog"
	"time"
)

// Every returns a Worker that calls fn at once and then every interval until the context ends, then returns nil.
// A failing fn is logged (slog.Default, with the worker's name) and tried again at the next tick, so it never
// stops the loop or the Manager. A tick that is still running when the next is due delays it: runs never overlap.
// A panic in fn is not caught here; the Manager recovers and restarts the worker.
//
//	app.Background(worker.Every("lead_followup_reminders", time.Minute, reminders.Tick))
//
//nolint:ireturn // the Worker interface is what Background takes
func Every(name string, interval time.Duration, fn func(ctx context.Context) error) Worker {
	return &every{name: name, interval: interval, fn: fn}
}

type every struct {
	name     string
	interval time.Duration
	fn       func(ctx context.Context) error
}

func (e *every) Name() string { return e.name }

func (e *every) Run(ctx context.Context) error {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return nil
		}

		err := e.fn(ctx)
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "worker: run failed", "worker", e.name, "error", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
