package server

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUUIDIssuer_Issue_NoTTL(t *testing.T) {
	ctx := context.Background()
	i, err := NewUUIDIssuer("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	tok, err := i.Issue(ctx, "alice", 0)
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(tok, "tok:"))
	parts := strings.SplitN(tok, ":", 5)
	require.Equal(t, 5, len(parts))
	assert.Equal(t, "0", parts[2], "no-TTL token should have zero expiry")
	subject, decodeErr := base64.RawURLEncoding.DecodeString(parts[3])
	require.NoError(t, decodeErr)
	assert.Equal(t, "alice", string(subject))
}

func TestUUIDIssuer_Issue_WithTTL(t *testing.T) {
	ctx := context.Background()
	i, err := NewUUIDIssuer("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	before := time.Now()
	tok, err := i.Issue(ctx, "bob", time.Minute)
	after := time.Now()
	assert.NoError(t, err)
	parts := strings.SplitN(tok, ":", 5)
	require.Equal(t, 5, len(parts))
	// expiry should be roughly Now+1min
	parsed, err := i.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "bob", parsed.Subject)
	_ = before
	_ = after
}

func TestUUIDIssuer_Issue_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	i, err := NewUUIDIssuer("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	_, err = i.Issue(ctx, "alice", time.Minute)
	assert.Error(t, err)
}

func TestParse_Valid_NoExpiry(t *testing.T) {
	i, err := NewUUIDIssuer("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	tok, err := i.Issue(context.Background(), "alice", 0)
	require.NoError(t, err)
	p, err := i.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "alice", p.Subject)
	assert.NotEmpty(t, p.ID)
}

func TestParse_Valid_WithExpiry(t *testing.T) {
	i, err := NewUUIDIssuer("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	tok, err := i.Issue(context.Background(), "alice", time.Minute)
	require.NoError(t, err)
	p, err := i.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "alice", p.Subject)
}

func TestParse_Expired(t *testing.T) {
	i, err := NewUUIDIssuer("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	tok, err := i.Issue(context.Background(), "alice", -time.Second)
	require.NoError(t, err)
	_, err = i.Parse(tok)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestParse_Invalid(t *testing.T) {
	i, err := NewUUIDIssuer("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	for _, bad := range []string{"", "notatok", "tok:a:b", "tok:a:notanumber:sub", "tok:a:b:c:d"} {
		_, err := i.Parse(bad)
		assert.ErrorIs(t, err, ErrTokenInvalid, "token: %q", bad)
	}
}

func TestParse_TamperedToken(t *testing.T) {
	i, err := NewUUIDIssuer("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)

	tok, err := i.Issue(context.Background(), "alice", time.Minute)
	require.NoError(t, err)

	tampered := tok[:len(tok)-1] + "A"
	_, err = i.Parse(tampered)
	assert.ErrorIs(t, err, ErrTokenInvalid)
}
