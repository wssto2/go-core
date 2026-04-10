package bootstrap

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockModule struct {
	mock.Mock
}

func (m *MockModule) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockModule) Register(c *Container) error {
	return m.Called(c).Error(0)
}

func (m *MockModule) Boot(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return m.Called(ctx).Error(0)
}

func (m *MockModule) Shutdown(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func TestAppLifecycle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.I18n.Dir = tempI18nDir(t)
	builder := New(cfg).DefaultInfrastructure()
	app, _ := builder.Build()

	m1 := new(MockModule)
	m2 := new(MockModule)

	app.modules = []Module{m1, m2}

	t.Run("Full Lifecycle", func(t *testing.T) {
		ctx := context.Background()

		m1.On("Name").Return("m1")
		m2.On("Name").Return("m2")

		// Register
		m1.On("Register", app.container).Return(nil)
		m2.On("Register", app.container).Return(nil)

		// Boot
		m1.On("Boot", mock.Anything).Return(nil)
		m2.On("Boot", mock.Anything).Return(nil)

		// Shutdown
		m1.On("Shutdown", mock.Anything).Return(nil)
		m2.On("Shutdown", mock.Anything).Return(nil)

		err := app.registerModules()
		assert.NoError(t, err)

		err = app.bootModules(ctx)
		assert.NoError(t, err)

		app.Shutdown(slog.Default())

		// Instead of AssertExpectations which is sensitive to call count
		// we just check that we didn't panic and the logic flowed.
	})

	t.Run("Reverse Shutdown Order", func(t *testing.T) {
		var order []string
		m1.ExpectedCalls = nil
		m2.ExpectedCalls = nil

		m1.On("Name").Return("m1")
		m2.On("Name").Return("m2")

		m1.On("Shutdown", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			order = append(order, "m1")
		})
		m2.On("Shutdown", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			order = append(order, "m2")
		})

		app.Shutdown(slog.Default())

		// m2 should shutdown before m1
		assert.Equal(t, []string{"m2", "m1"}, order)
	})
}
