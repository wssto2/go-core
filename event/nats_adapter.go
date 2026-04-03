package event

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"sync"

	"github.com/wssto2/go-core/apperr"
)

// NatsClient is an abstract interface representing the minimal subset of a NATS client
// needed by this adapter. This avoids pulling in a concrete NATS dependency in-core.
type NatsClient interface {
	Publish(subject string, data []byte) error
	Subscribe(subject string, handler func(data []byte)) (Subscription, error)
}

// Subscription represents a subscription returned by the client.
type Subscription interface {
	Unsubscribe() error
}

// NATSBus is a minimal adapter that publishes events as JSON to a subject derived
// from the Go type (reflect.Type.String()). Subscriptions decode JSON back into the
// original type and invoke the provided handler.
type NATSBus struct {
	client NatsClient
	log    *slog.Logger

	mu   sync.Mutex
	subs map[string]Subscription
}

// Ping verifies the NATS connection by publishing a zero-byte probe to a
// dedicated health subject. Returns an error if the client is disconnected.
func (n *NATSBus) Ping(ctx context.Context) error {
	return n.client.Publish("_health.ping", nil)
}

// NewNATSBus constructs a NATS-backed Bus adapter.
func NewNATSBus(client NatsClient, log *slog.Logger) *NATSBus {
	return &NATSBus{
		client: client,
		log:    log,
		subs:   make(map[string]Subscription),
	}
}

// Publish marshals the event as JSON and publishes to a subject derived from the type.
// Events are wrapped in an Envelope carrying request_id, timestamp and source so
// downstream consumers always receive the required metadata.
func (n *NATSBus) Publish(ctx context.Context, event any) error {
	env, err := WrapEventWithMetadata(ctx, event)
	if err != nil {
		return apperr.Internal(err)
	}
	data, err := json.Marshal(env)
	if err != nil {
		return apperr.Internal(err)
	}
	subject := reflect.TypeOf(event).String()
	if err := n.client.Publish(subject, data); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// unmarshalIntoType unmarshals JSON data into a newly allocated value of type t.
// For pointer types (t.Kind() == reflect.Ptr) it allocates and returns the pointer.
// For value types it allocates a pointer, unmarshals, and returns the dereferenced value.
func unmarshalIntoType(data []byte, t reflect.Type) (any, error) {
	if t.Kind() == reflect.Ptr {
		vptr := reflect.New(t.Elem())
		if err := json.Unmarshal(data, vptr.Interface()); err != nil {
			return nil, err
		}
		return vptr.Interface(), nil
	}
	vptr := reflect.New(t)
	if err := json.Unmarshal(data, vptr.Interface()); err != nil {
		return nil, err
	}
	return vptr.Elem().Interface(), nil
}

// Subscribe registers a subscription for the given event type. The provided event
// parameter is only used for its type (pass a zero value). Handlers are invoked with
// a background context since NATS delivery is asynchronous.
// If a subscription for the same subject already exists it is unsubscribed first,
// preventing duplicate handler invocations on re-registration.
func (n *NATSBus) Subscribe(event any, handler func(ctx context.Context, event any) error) error {
	t := reflect.TypeOf(event)
	subject := t.String()

	n.mu.Lock()
	if existing, ok := n.subs[subject]; ok {
		_ = existing.Unsubscribe()
		delete(n.subs, subject)
	}
	n.mu.Unlock()

	sub, err := n.client.Subscribe(subject, func(data []byte) {
		// Attempt to decode an Envelope first (newer messages). If it isn't an
		// envelope, fall back to decoding the raw event JSON for backward compatibility.
		var recv any
		var env Envelope
		var id string
		if err := json.Unmarshal(data, &env); err == nil && env.Version == "1" {
			id = env.RequestID
			payload := env.Payload
			var unmarshalErr error
			recv, unmarshalErr = unmarshalIntoType(payload, t)
			if unmarshalErr != nil {
				if n.log != nil {
					n.log.Error("NATSBus: failed to unmarshal event payload", "error", unmarshalErr)
				}
				return
			}
		} else {
			var unmarshalErr error
			recv, unmarshalErr = unmarshalIntoType(data, t)
			if unmarshalErr != nil {
				if n.log != nil {
					n.log.Error("NATSBus: failed to unmarshal event", "error", unmarshalErr)
				}
				return
			}
		}
		// prepare context with event id if present
		ctx2 := context.Background()
		if id != "" {
			ctx2 = context.WithValue(ctx2, eventRequestIDKey, id)
		}
		// invoke handler; errors are logged because delivery is async
		if err := handler(ctx2, recv); err != nil {
			if n.log != nil {
				n.log.Error("NATSBus: handler error", "error", err)
			}
		}
	})
	if err != nil {
		return apperr.Internal(err)
	}

	n.mu.Lock()
	if _, ok := n.subs[subject]; ok {
		// Another goroutine concurrently registered a subscription for the same subject.
		// Discard ours to avoid a leak; the other subscription is already stored.
		n.mu.Unlock()
		_ = sub.Unsubscribe()
		return nil
	}
	n.subs[subject] = sub
	n.mu.Unlock()
	return nil
}

// Close unsubscribes all tracked subscriptions. Call this during application shutdown.
func (n *NATSBus) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	var lastErr error
	for subject, sub := range n.subs {
		if err := sub.Unsubscribe(); err != nil {
			lastErr = err
		}
		delete(n.subs, subject)
	}
	return lastErr
}
