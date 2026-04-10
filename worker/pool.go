package worker

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"
)

// ErrQueueFull is returned when the pool queue is full.
var ErrQueueFull = errors.New("queue is full")

// Pool is a bounded worker pool. It runs a fixed number of worker goroutines and
// accepts jobs up to a fixed queue size. Submitting a job never spawns a new
// goroutine; if the queue is full Submit returns ErrQueueFull.
type Pool struct {
	workers int
	queue   chan func(context.Context) error
	wg      sync.WaitGroup
	mu      sync.Mutex
	started bool
	closed  bool
	logger  *slog.Logger
	metrics Metrics
}

// Options holds configuration for the pool constructed via functional options.
type Options struct {
	Workers   int
	QueueSize int
	Logger    *slog.Logger
	Metrics   Metrics
}

// Option applies a configuration to Options.
type Option func(*Options)

// WithWorkers sets the number of workers.
func WithWorkers(n int) Option {
	return func(o *Options) { o.Workers = n }
}

// WithQueueSize sets the queue size.
func WithQueueSize(n int) Option {
	return func(o *Options) { o.QueueSize = n }
}

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option {
	return func(o *Options) { o.Logger = l }
}

// WithMetrics sets the metrics implementation.
func WithMetrics(m Metrics) Option {
	return func(o *Options) { o.Metrics = m }
}

// New creates a new Pool using functional options. Defaults: workers=1, queueSize=workers, logger=slog.Default().
func New(opts ...Option) *Pool {
	options := &Options{Workers: 1}
	for _, o := range opts {
		o(options)
	}
	if options.Workers <= 0 {
		options.Workers = 1
	}
	if options.QueueSize <= 0 {
		options.QueueSize = options.Workers
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.Metrics == nil {
		options.Metrics = NewNoopMetrics()
	}
	return &Pool{
		workers: options.Workers,
		queue:   make(chan func(context.Context) error, options.QueueSize),
		logger:  options.Logger,
		metrics: options.Metrics,
	}
}

// Start launches the worker goroutines. Start is idempotent.
func (p *Pool) Start(ctx context.Context) {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return
	}
	p.started = true
	p.mu.Unlock()

	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.workerLoop(ctx, i)
	}
}

func (p *Pool) workerLoop(ctx context.Context, id int) {
	defer p.wg.Done()
	p.logger.Info("worker_pool.worker_starting", "id", id)
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("worker_pool.worker_stopping", "id", id)
			return
		case job, ok := <-p.queue:
			if !ok {
				p.logger.Info("worker_pool.queue_closed", "id", id)
				return
			}
			p.executeJob(ctx, job)
		}
	}
}

func (p *Pool) executeJob(ctx context.Context, job func(context.Context) error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			p.logger.Error("worker_pool.job_panic", "panic", r, "stack", string(stack))
			p.metrics.TasksPanicked(ctx)
		}
	}()
	if err := job(ctx); err != nil {
		p.logger.Error("worker_pool.job_error", "error", err)
	}
}

// Submit enqueues a job for execution. If the queue is full, ErrQueueFull is returned.
// Submit never spawns goroutines; it either enqueues the job or returns an error.
// After Close is called, Submit returns ErrQueueFull immediately.
func (p *Pool) Submit(ctx context.Context, job func(context.Context) error) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrQueueFull
	}
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.queue <- job:
		p.metrics.TasksSubmitted(ctx)
		return nil
	default:
		p.metrics.TasksRejected(ctx)
		return ErrQueueFull
	}
}

// Wait waits until all worker goroutines have exited (after the start context
// has been cancelled or Close has been called).
func (p *Pool) Wait() {
	p.wg.Wait()
}

// Close closes the job queue, signalling workers to drain any remaining jobs
// and then exit. It is safe to call Close concurrently and it is idempotent.
// After Close, Submit will return ErrQueueFull for new jobs.
// Call Wait after Close to block until all workers have stopped.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started && !p.closed {
		p.closed = true
		close(p.queue)
	}
}
