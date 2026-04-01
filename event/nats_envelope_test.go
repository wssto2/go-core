package event

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/wssto2/go-core/observability/tracing"
)

func TestNATSBus_EnvelopeMetadata(t *testing.T) {
	client := newFakeNatsClient()
	bus := NewNATSBus(client, nil)

	type MyEvent struct {
		Message string `json:"message"`
	}

	var wg sync.WaitGroup
	wg.Add(1)

	var captured []byte
	subject := reflect.TypeOf(MyEvent{}).String()
	_, err := client.Subscribe(subject, func(data []byte) {
		captured = append([]byte{}, data...)
		wg.Done()
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	tr := tracing.NewSimpleTracer()
	ctx, finish := tr.StartSpan(context.Background(), "test")
	defer finish(nil)

	if err := bus.Publish(ctx, MyEvent{Message: "hello"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	wg.Wait()

	var env Envelope
	if err := json.Unmarshal(captured, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if env.RequestID == "" {
		t.Fatalf("expected request_id in envelope")
	}
	if env.Source != DefaultEventSource {
		t.Fatalf("expected source %q, got %q", DefaultEventSource, env.Source)
	}
	if env.Timestamp.IsZero() {
		t.Fatalf("expected non-zero timestamp")
	}

	var payload MyEvent
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Message != "hello" {
		t.Fatalf("unexpected payload: %v", payload)
	}
}
