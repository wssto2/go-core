package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithDefaults_CORS_NoWildcard(t *testing.T) {
	cfg := Config{}.withDefaults()
	for _, origin := range cfg.Cors.AllowOrigins {
		assert.NotEqual(t, "*", origin, "default CORS must not allow all origins with wildcard")
	}
}
