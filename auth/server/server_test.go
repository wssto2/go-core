package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUUIDIssuer_Issue(t *testing.T) {
	ctx := context.Background()
	i := NewUUIDIssuer()
	tok, err := i.Issue(ctx, "alice", time.Minute)
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(tok, "tok:"))
	parts := strings.SplitN(tok, ":", 3)
	assert.Equal(t, 3, len(parts))
	assert.Equal(t, "alice", parts[2])
}
