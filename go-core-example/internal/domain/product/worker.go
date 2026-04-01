package product

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wssto2/go-core/event"
)

// Simple worker that listens to ProductCreatedEvent and logs it.
type productWorker struct {
	bus event.Bus
	log *slog.Logger
}

func NewProductWorker(bus event.Bus, log *slog.Logger) (*productWorker, error) {
	w := &productWorker{log: log}
	if err := bus.Subscribe(ProductCreatedEvent{}, w.handleProductCreated); err != nil {
		return nil, fmt.Errorf("product worker: subscribe: %w", err)
	}
	return w, nil
}

func (w *productWorker) handleProductCreated(ctx context.Context, e any) error {
	ev, ok := e.(ProductCreatedEvent)
	if !ok {
		return fmt.Errorf("unexpected event type %T", e)
	}
	w.log.Info("product created event received", "id", ev.ProductID, "sku", ev.SKU)
	return nil
}

func (w *productWorker) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (w *productWorker) Name() string {
	return "product-event-listener"
}
