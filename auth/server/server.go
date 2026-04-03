// Package server provides the authentication server implementation.
package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrTokenExpired is returned when a token's TTL has elapsed.
var ErrTokenExpired = errors.New("token expired")

// ErrTokenInvalid is returned when a token cannot be parsed.
var ErrTokenInvalid = errors.New("token invalid")

// Issuer issues tokens for subjects.
type Issuer interface {
	Issue(ctx context.Context, subject string, ttl time.Duration) (string, error)
}

// UUIDIssuer issues tokens that include a UUID, an optional expiry, and the subject.
// Token format: "tok:<uuid>:<expiry_unix_ns>:<subject>"
// expiry_unix_ns is 0 when ttl is zero (meaning no expiry).
type UUIDIssuer struct{}

// NewUUIDIssuer constructs a UUIDIssuer.
func NewUUIDIssuer() *UUIDIssuer { return &UUIDIssuer{} }

// Issue issues a token. It returns early if ctx is canceled.
// When ttl > 0 the expiry time is embedded in the token and enforced by Parse.
func (u *UUIDIssuer) Issue(ctx context.Context, subject string, ttl time.Duration) (string, error) {
	if ctx.Err() != nil {
		return "", fmt.Errorf("context canceled: %w", ctx.Err())
	}

	id := uuid.New().String()
	var expiryNs int64
	if ttl != 0 {
		expiryNs = time.Now().Add(ttl).UnixNano()
	}

	return fmt.Sprintf("tok:%s:%d:%s", id, expiryNs, subject), nil
}

// ParsedToken holds the verified fields of a token issued by UUIDIssuer.
type ParsedToken struct {
	ID      string
	Subject string
}

// Parse validates the token format and, when an expiry was embedded, checks
// that it has not elapsed. It returns ErrTokenInvalid for malformed tokens
// and ErrTokenExpired when the TTL has passed.
func Parse(token string) (*ParsedToken, error) {
	// expected format: tok:<uuid>:<expiry_unix_ns>:<subject>
	if !strings.HasPrefix(token, "tok:") {
		return nil, ErrTokenInvalid
	}
	parts := strings.SplitN(token, ":", 4)
	if len(parts) != 4 {
		return nil, ErrTokenInvalid
	}
	id := parts[1]
	expiryNs, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, ErrTokenInvalid
	}
	subject := parts[3]
	if id == "" || subject == "" {
		return nil, ErrTokenInvalid
	}
	if expiryNs != 0 && time.Now().UnixNano() > expiryNs {
		return nil, ErrTokenExpired
	}
	return &ParsedToken{ID: id, Subject: subject}, nil
}
