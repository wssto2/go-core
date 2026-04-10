package product

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/httpclient"
)

// NewWebhookPublisher returns an event.PublishFunc that:
//   - Forwards ProductCreatedEvent to an external webhook (if baseURL is set).
//   - Re-publishes ProductImageUploadedEvent onto the in-process bus so
//     imageWorker handles it on the outbox crash-recovery path.
//
// Pass this directly to bootstrap.WithOutboxWorker or event.NewOutboxWorker.
func NewWebhookPublisher(baseURL, token string, bus event.Bus) event.PublishFunc {
	var client *httpclient.Client
	if baseURL != "" {
		client = httpclient.New(baseURL,
			httpclient.WithAuth(httpclient.BearerAuth{Token: token}),
			httpclient.WithRetry(3, 200*time.Millisecond),
			httpclient.WithCircuitBreaker(5, 30*time.Second),
		)
	}

	return func(ctx context.Context, subject string, data []byte) error {
		switch subject {
		case "product.ProductCreatedEvent":
			if client == nil {
				return nil
			}
			_, err := client.Post(ctx, "/webhooks/product-created", data, nil)
			return err

		case "product.ProductImageUploadedEvent":
			if bus == nil {
				return nil
			}
			// Decode the outbox envelope to recover the original event payload.
			var env event.Envelope
			if err := json.Unmarshal(data, &env); err != nil {
				slog.WarnContext(ctx, "outbox: failed to decode image event envelope", "err", err)
				return nil
			}
			var ev ProductImageUploadedEvent
			if err := json.Unmarshal(env.Payload, &ev); err != nil {
				slog.WarnContext(ctx, "outbox: failed to decode image event payload", "err", err)
				return nil
			}
			if err := bus.Publish(ctx, ev); err != nil {
				slog.WarnContext(ctx, "outbox: failed to re-publish image event to bus", "err", err)
			}
			return nil

		default:
			slog.WarnContext(ctx, "outbox: unhandled event type", "subject", subject)
			return nil
		}
	}
}
