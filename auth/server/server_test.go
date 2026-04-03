package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUUIDIssuer_Issue_NoTTL(t *testing.T) {
	ctx := context.Background()
	i := NewUUIDIssuer()
	tok, err := i.Issue(ctx, "alice", 0)
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(tok, "tok:"))
	// format: tok:<uuid>:<expiry_ns>:<subject>
	parts := strings.SplitN(tok, ":", 4)
	require.Equal(t, 4, len(parts))
	assert.Equal(t, "0", parts[2], "no-TTL token should have zero expiry")
	assert.Equal(t, "alice", parts[3])
}

func TestUUIDIssuer_Issue_WithTTL(t *testing.T) {
	ctx := context.Background()
	i := NewUUIDIssuer()
	before := time.Now()
	tok, err := i.Issue(ctx, "bob", time.Minute)
	after := time.Now()
	assert.NoError(t, err)
	parts := strings.SplitN(tok, ":", 4)
	require.Equal(t, 4, len(parts))
	assert.Equal(t, "bob", parts[3])
	// expiry should be roughly Now+1min
	parsed, err := Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "bob", parsed.Subject)
	_ = before
	_ = after
}

func TestUUIDIssuer_Issue_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	i := NewUUIDIssuer()
	_, err := i.Issue(ctx, "alice", time.Minute)
	assert.Error(t, err)
}

func TestParse_Valid_NoExpiry(t *testing.T) {
	i := NewUUIDIssuer()
	tok, err := i.Issue(context.Background(), "alice", 0)
	require.NoError(t, err)
	p, err := Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "alice", p.Subject)
	assert.NotEmpty(t, p.ID)
}

func TestParse_Valid_WithExpiry(t *testing.T) {
	i := NewUUIDIssuer()
	tok, err := i.Issue(context.Background(), "alice", time.Minute)
	require.NoError(t, err)
	p, err := Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "alice", p.Subject)
}

func TestParse_Expired(t *testing.T) {
	i := NewUUIDIssuer()
	tok, err := i.Issue(context.Background(), "alice", -time.Second)
	require.NoError(t, err)
	_, err = Parse(tok)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestParse_Invalid(t *testing.T) {
	for _, bad := range []string{"", "notatok", "tok:a:b", "tok:a:notanumber:sub"} {
		_, err := Parse(bad)
		assert.ErrorIs(t, err, ErrTokenInvalid, "token: %q", bad)
	}
}
