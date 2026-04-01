package bootstrap

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
)

type cancelTestModule struct {
	mock.Mock
	name   string
	bootFn func(ctx context.Context) error
}

func (m *cancelTestModule) Name() string                { return m.name }
func (m *cancelTestModule) Register(_ *Container) error { return nil }
func (m *cancelTestModule) Boot(ctx context.Context) error {
	if m.bootFn != nil {
		return m.bootFn(ctx)
	}
	return nil
}
func (m *cancelTestModule) Shutdown(ctx context.Context) error { return nil }

func TestBootModules_CancelsOthersOnFirstFailure(t *testing.T) {
	failModule := &cancelTestModule{
		name: "fail",
		bootFn: func(ctx context.Context) error {
			return fmt.Errorf("intentional boot failure")
		},
	}
	blockedCh := make(chan struct{})
	blockModule := &cancelTestModule{
		name: "block",
		bootFn: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				close(blockedCh)
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return fmt.Errorf("blockModule was not cancelled within 5s")
			}
		},
	}
	app := NewApp(DefaultConfig(), NewContainer(), nil, nil, []Module{failModule, blockModule})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := app.bootModules(ctx)
	if err == nil {
		t.Fatal("expected bootModules to return error, got nil")
	}
	select {
	case <-blockedCh:
		// blockModule received cancellation — correct
	case <-time.After(2 * time.Second):
		t.Fatal("blockModule was not cancelled within 2s — context not propagated")
	}
}
