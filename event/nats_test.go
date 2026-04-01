package event

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

// fakeNatsClient is a tiny in-memory standin used for tests.
type fakeNatsClient struct {
	mu   sync.Mutex
	subs map[string][]func([]byte)
}

func newFakeNatsClient() *fakeNatsClient {
	return &fakeNatsClient{subs: make(map[string][]func([]byte))}
}

func (f *fakeNatsClient) Publish(subject string, data []byte) error {
	f.mu.Lock()
	subs := append([]func([]byte){}, f.subs[subject]...)
	f.mu.Unlock()
	for _, cb := range subs {
		cb(data)
	}
	return nil
}

func (f *fakeNatsClient) Subscribe(subject string, handler func([]byte)) (Subscription, error) {
	f.mu.Lock()
	f.subs[subject] = append(f.subs[subject], handler)
	f.mu.Unlock()
	return &fakeSubscription{client: f, subject: subject, handler: handler}, nil
}

type fakeSubscription struct {
	client  *fakeNatsClient
	subject string
	handler func([]byte)
}

func (s *fakeSubscription) Unsubscribe() error {
	s.client.mu.Lock()
	defer s.client.mu.Unlock()
	subs := s.client.subs[s.subject]
	for i, cb := range subs {
		if reflect.ValueOf(cb).Pointer() == reflect.ValueOf(s.handler).Pointer() {
			s.client.subs[s.subject] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	return nil
}

// Test value-type subscription
func TestNATSBus_ValueType(t *testing.T) {
	client := newFakeNatsClient()
	bus := NewNATSBus(client, nil)

	type MyEvent struct{ Message string }

	var wg sync.WaitGroup
	wg.Add(1)

	if err := bus.Subscribe(MyEvent{}, func(ctx context.Context, event any) error {
		ev, ok := event.(MyEvent)
		if !ok {
			t.Fatalf("expected MyEvent value, got %T", event)
		}
		if ev.Message != "hello" {
			t.Fatalf("unexpected message: %s", ev.Message)
		}
		wg.Done()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := bus.Publish(context.Background(), MyEvent{Message: "hello"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	wg.Wait()
}

// Test pointer-type subscription
func TestNATSBus_PointerType(t *testing.T) {
	client := newFakeNatsClient()
	bus := NewNATSBus(client, nil)

	type MyEvent struct{ Message string }

	var wg sync.WaitGroup
	wg.Add(1)

	if err := bus.Subscribe(&MyEvent{}, func(ctx context.Context, event any) error {
		ev, ok := event.(*MyEvent)
		if !ok {
			t.Fatalf("expected *MyEvent pointer, got %T", event)
		}
		if ev.Message != "world" {
			t.Fatalf("unexpected message: %s", ev.Message)
		}
		wg.Done()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := bus.Publish(context.Background(), &MyEvent{Message: "world"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	wg.Wait()
}
