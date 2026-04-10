package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/observability"
	"github.com/wssto2/go-core/worker"
)

// outboxModule is a minimal Module wired by WithOutboxWorker.
// It polls the outbox table and forwards events via the provided PublishFunc.
type outboxModule struct {
	publish event.PublishFunc
	opts    []event.WorkerOption
	mgr     *worker.Manager
}

func newOutboxModule(publish event.PublishFunc, opts []event.WorkerOption) *outboxModule {
	return &outboxModule{publish: publish, opts: opts}
}

func (m *outboxModule) Name() string { return "outbox" }

func (m *outboxModule) Register(c *Container) error {
	log := MustResolve[*slog.Logger](c)
	tel := MustResolve[*observability.Telemetry](c)
	db := MustResolve[*database.Registry](c).Primary()

	if err := event.EnsureOutboxSchema(db); err != nil {
		return fmt.Errorf("outbox: migrate: %w", err)
	}

	w := event.NewOutboxWorker(db, m.publish, log, 500*time.Millisecond, 50, m.opts...)
	m.mgr = worker.NewManager(log, worker.WithManagerMetrics(tel.Worker))
	m.mgr.Add(w)
	return nil
}

func (m *outboxModule) Boot(ctx context.Context) error {
	m.mgr.Start(ctx)
	return nil
}

func (m *outboxModule) Shutdown(ctx context.Context) error {
	if m.mgr == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		m.mgr.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("outbox: shutdown timed out: %w", ctx.Err())
	}
}
