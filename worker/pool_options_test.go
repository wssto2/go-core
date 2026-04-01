package worker

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPool_NewWithOptions(t *testing.T) {
	t.Parallel()

	p := New(WithWorkers(3), WithQueueSize(7), WithLogger(slog.Default()))
	assert.Equal(t, 3, p.workers)
	assert.Equal(t, 7, cap(p.queue))
	assert.NotNil(t, p.logger)
}

func TestPool_NewDefaults(t *testing.T) {
	t.Parallel()

	p := New()
	assert.Equal(t, 1, p.workers)
	assert.Equal(t, 1, cap(p.queue))
	assert.NotNil(t, p.logger)
}
