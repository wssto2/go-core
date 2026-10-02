package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
)

type recordingModule struct {
	name    string
	bootErr error
	stopped *[]string
	mu      *sync.Mutex
}

func (m recordingModule) Name() string                { return m.name }
func (m recordingModule) Register(_ *Container) error { return nil }
func (m recordingModule) Boot(context.Context) error  { return m.bootErr }
func (m recordingModule) Shutdown(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.stopped = append(*m.stopped, m.name)

	return nil
}

func TestRunContext_BootFailureStopsBootedModulesInReverse(t *testing.T) {
	var (
		stopped []string
		mu      sync.Mutex
	)

	mod := func(name string, err error) Module {
		return recordingModule{name: name, bootErr: err, stopped: &stopped, mu: &mu}
	}

	c := NewContainer()
	Bind(c, slog.New(slog.DiscardHandler))

	app := NewApp(DefaultConfig(), c, nil, nil, []Module{mod("a", nil), mod("b", nil), mod("c", errors.New("boom"))})
	if err := app.RunContext(context.Background()); err == nil {
		t.Fatal("want the boot error")
	}

	if len(stopped) != 2 || stopped[0] != "b" || stopped[1] != "a" {
		t.Fatalf("want [b a] stopped, got %v", stopped)
	}
}
