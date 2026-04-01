package event

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInMemoryBus_ConcurrentPublish(t *testing.T) {
	b := NewInMemoryBus()
	var calls int32
	// subscribe a single handler
	_ = b.Subscribe(struct{ Name string }{}, func(ctx context.Context, event any) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	const N = 500
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			_ = b.Publish(context.Background(), struct{ Name string }{Name: "foo"})
			wg.Done()
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(N), atomic.LoadInt32(&calls))
}

func TestInMemoryBus_ConcurrentSubscribeAndPublish(t *testing.T) {
	b := NewInMemoryBus()
	var calls int32

	nSubscribers := 50
	nPublishers := 100

	var subsWg sync.WaitGroup
	subsWg.Add(nSubscribers)
	for i := 0; i < nSubscribers; i++ {
		go func() {
			_ = b.Subscribe(struct{ Name string }{}, func(ctx context.Context, event any) error {
				atomic.AddInt32(&calls, 1)
				return nil
			})
			subsWg.Done()
		}()
	}
	subsWg.Wait()

	var pubWg sync.WaitGroup
	pubWg.Add(nPublishers)
	for i := 0; i < nPublishers; i++ {
		go func() {
			_ = b.Publish(context.Background(), struct{ Name string }{Name: "bar"})
			pubWg.Done()
		}()
	}
	pubWg.Wait()

	expected := int32(nSubscribers * nPublishers)
	assert.Equal(t, expected, atomic.LoadInt32(&calls))
}
