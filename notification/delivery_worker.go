package notification

import (
	"context"
	"time"
)

// deliveryWorker sends the due e-mails (NOTIF-DELIVERY-001). When a batch was full it goes again at once instead of
// waiting for the next tick, so a burst drains quickly. Install adds it to the application's background work when
// identity has mail.
type deliveryWorker struct{ notices *Notices }

// Name identifies the worker in logs and metrics.
func (w *deliveryWorker) Name() string { return "notification.delivery" }

// Run delivers until ctx is cancelled.
func (w *deliveryWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		handled, err := w.notices.DeliverDue(ctx)
		if err != nil && ctx.Err() == nil {
			w.notices.log.WarnContext(ctx, "notification: e-mail delivery errors", "handled", handled, "error", err)
		}

		if handled >= deliveryBatch && err == nil && ctx.Err() == nil {
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
