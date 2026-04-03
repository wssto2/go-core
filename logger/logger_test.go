package logger

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetFromContext_WrongType_ReturnsDefault(t *testing.T) {
	// Store an integer under the context key — wrong type.
	ctx := context.WithValue(context.Background(), ctxKey{}, 42)

	// Must not panic; must return the default logger.
	got := GetFromContext(ctx)
	assert.Same(t, slog.Default(), got, "should fall back to slog.Default() on type mismatch")
}

func TestGetFromContext_NoValue_ReturnsDefault(t *testing.T) {
	got := GetFromContext(context.Background())
	assert.Same(t, slog.Default(), got)
}
