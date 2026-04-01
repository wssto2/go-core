package bootstrap

import (
	"context"
	"testing"
)

type countingModule struct {
	name          string
	registerCount int
	bootCount     int
}

func (m *countingModule) Name() string { return m.name }
func (m *countingModule) Register(_ *Container) error {
	m.registerCount++
	return nil
}
func (m *countingModule) Boot(_ context.Context) error {
	m.bootCount++
	return nil
}
func (m *countingModule) Shutdown(_ context.Context) error { return nil }

func TestAppBuilder_ModuleRegisteredExactlyOnce(t *testing.T) {
	m := &countingModule{name: "counting"}

	cfg := DefaultConfig()
	builder := New(cfg).DefaultInfrastructure()
	app, err := builder.WithModules(m).Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	// Build() must NOT register. registerCount must still be 0.
	if m.registerCount != 0 {
		t.Fatalf("Build() registered module %d time(s), want 0", m.registerCount)
	}

	_, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so Run() shuts down fast

	// Instead of app.Run(), call registerModules and bootModules directly to avoid blocking on signal.
	if err := app.registerModules(); err != nil {
		t.Fatalf("registerModules() failed: %v", err)
	}
	// No context cancellation needed for this test
	_ = app.bootModules(context.Background())

	if m.registerCount != 1 {
		t.Fatalf("registerModules() registered module %d time(s), want 1", m.registerCount)
	}
}
