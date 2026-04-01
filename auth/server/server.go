// Package server provides the authentication server implementation.
package server

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Issuer issues tokens for subjects.
type Issuer interface {
	Issue(ctx context.Context, subject string, ttl time.Duration) (string, error)
}

// UUIDIssuer issues tokens that include a UUID and the subject.
// Token format: "tok:<uuid>:<subject>".
type UUIDIssuer struct{}

// NewUUIDIssuer constructs a UUIDIssuer.
func NewUUIDIssuer() *UUIDIssuer { return &UUIDIssuer{} }

// Issue issues a token. It returns early if ctx is canceled.
func (u *UUIDIssuer) Issue(ctx context.Context, subject string, ttl time.Duration) (string, error) {
	if ctx.Err() != nil {
		return "", fmt.Errorf("context canceled: %w", ctx.Err())
	}

	id := uuid.New().String()
	_ = ttl // currently unused, reserved for future expiry handling

	return fmt.Sprintf("tok:%s:%s", id, subject), nil
}
