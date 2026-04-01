package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ModuleService is a small service that modules are expected to Resolve from the Container.
type ModuleService interface {
	Do() string
}

type moduleServiceImpl struct{}

func (s *moduleServiceImpl) Do() string { return "done" }

// TestModule demonstrates a Module implementation that resolves services from the Container.
type TestModule struct {
	Registered bool
	Result     string
}

func (m *TestModule) Name() string { return "test-module" }
func (m *TestModule) Register(c *Container) error {
	svc, err := Resolve[ModuleService](c)
	if err != nil {
		return err
	}
	m.Result = svc.Do()
	m.Registered = true
	return nil
}
func (m *TestModule) Boot(ctx context.Context) error     { return nil }
func (m *TestModule) Shutdown(ctx context.Context) error { return nil }

func TestModuleUsesContainer(t *testing.T) {
	c := &Container{}
	Bind[ModuleService](c, &moduleServiceImpl{})
	m := &TestModule{}

	err := m.Register(c)
	assert.NoError(t, err)
	assert.True(t, m.Registered)
	assert.Equal(t, "done", m.Result)
}
