package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfig_EmptyJWTSecret_FailsValidation verifies that LoadConfig returns a
// validation error when JWT_SECRET is not set (empty string).
func TestConfig_EmptyJWTSecret_FailsValidation(t *testing.T) {
	cfg := DefaultConfig()
	err := LoadConfig(&cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}
