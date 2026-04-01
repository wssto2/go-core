package worker

import "context"

// Metrics defines counters for the worker pool.
type Metrics interface {
	TasksSubmitted(ctx context.Context)
	TasksRejected(ctx context.Context)
	TasksPanicked(ctx context.Context)
}

// NewNoopMetrics returns a no-op implementation.
func NewNoopMetrics() Metrics { return &noopMetrics{} }

type noopMetrics struct{}

func (n *noopMetrics) TasksSubmitted(ctx context.Context) {}
func (n *noopMetrics) TasksRejected(ctx context.Context)  {}
func (n *noopMetrics) TasksPanicked(ctx context.Context)  {}
