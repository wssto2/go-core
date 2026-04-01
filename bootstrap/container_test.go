package bootstrap_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wssto2/go-core/bootstrap"
)

type MyService interface {
	Do() string
}

type myServiceImpl struct{}

func (s *myServiceImpl) Do() string {
	return "done"
}

func TestContainer(t *testing.T) { //nolint:funlen
	t.Parallel()

	t.Run("Bind and Resolve", func(t *testing.T) {
		t.Parallel()

		c := &bootstrap.Container{}
		service := &myServiceImpl{}

		bootstrap.Bind[MyService](c, service)

		resolved, err := bootstrap.Resolve[MyService](c)
		assert.NoError(t, err)
		assert.Equal(t, service, resolved)
		assert.Equal(t, "done", resolved.Do())
	})

	t.Run("Resolve not found", func(t *testing.T) {
		t.Parallel()

		c := &bootstrap.Container{}

		resolved, err := bootstrap.Resolve[MyService](c)
		assert.Error(t, err)
		assert.Nil(t, resolved)
		assert.Contains(t, err.Error(), "service not found")
	})

	t.Run("MustResolve", func(t *testing.T) {
		t.Parallel()

		c := &bootstrap.Container{}
		service := &myServiceImpl{}

		bootstrap.Bind[MyService](c, service)

		resolved := bootstrap.MustResolve[MyService](c)
		assert.Equal(t, service, resolved)
	})

	t.Run("Thread safety", func(t *testing.T) {
		t.Parallel()

		container := &bootstrap.Container{}
		const iterations = 100

		done := make(chan bool)

		for i := range iterations {
			go func(val int) {
				bootstrap.OverwriteBind(container, val)

				done <- true
			}(i)
		}

		for range iterations {
			<-done
		}

		_, err := bootstrap.Resolve[int](container)
		assert.NoError(t, err)
	})

	t.Run("Strict duplicate bind panics", func(t *testing.T) {
		t.Parallel()

		c := &bootstrap.Container{}
		c.EnableStrictMode()
		service := &myServiceImpl{}
		bootstrap.Bind[MyService](c, service)
		assert.Panics(t, func() { bootstrap.Bind[MyService](c, service) })
	})

	t.Run("Resolve missing panics in strict mode", func(t *testing.T) {
		t.Parallel()

		c := &bootstrap.Container{}
		c.EnableStrictMode()
		assert.Panics(t, func() { _, _ = bootstrap.Resolve[MyService](c) })
	})
}
