// Package server provides the authentication server implementation.
package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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

// UUIDIssuer issues HMAC-signed tokens that include a UUID, an optional expiry,
// and the subject.
// Token format: "tok:<uuid>:<expiry_unix_ns>:<subject_b64url>:<signature_b64url>"
// expiry_unix_ns is 0 when ttl is zero (meaning no expiry).
type UUIDIssuer struct {
	signingKey []byte
}

// NewUUIDIssuer constructs a UUIDIssuer with the provided signing secret.
func NewUUIDIssuer(secret string) (*UUIDIssuer, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("uuid issuer secret must be at least 32 bytes")
	}
	return &UUIDIssuer{signingKey: []byte(secret)}, nil
}

// NewRandomUUIDIssuer constructs a UUIDIssuer with a random process-local secret.
func NewRandomUUIDIssuer() (*UUIDIssuer, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate signing key: %w", err)
	}
	return &UUIDIssuer{signingKey: key}, nil
}

// Issue issues a token. It returns early if ctx is canceled.
// When ttl > 0 the expiry time is embedded in the token and enforced by Parse.
func (u *UUIDIssuer) Issue(ctx context.Context, subject string, ttl time.Duration) (string, error) {
	if ctx.Err() != nil {
		return "", fmt.Errorf("context canceled: %w", ctx.Err())
	}
	if u == nil || len(u.signingKey) == 0 {
		return "", ErrTokenInvalid
	}
	if subject == "" {
		return "", ErrTokenInvalid
	}

	id := uuid.New().String()
	var expiryNs int64
	if ttl != 0 {
		expiryNs = time.Now().Add(ttl).UnixNano()
	}
	encodedSubject := base64.RawURLEncoding.EncodeToString([]byte(subject))
	payload := fmt.Sprintf("tok:%s:%d:%s", id, expiryNs, encodedSubject)
	return payload + ":" + u.sign(payload), nil
}

// ParsedToken holds the verified fields of a token issued by UUIDIssuer.
type ParsedToken struct {
	ID      string
	Subject string
}

func (u *UUIDIssuer) sign(payload string) string {
	mac := hmac.New(sha256.New, u.signingKey)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Parse validates the token format and signature and, when an expiry was
// embedded, checks that it has not elapsed. It returns ErrTokenInvalid for
// malformed or tampered tokens and ErrTokenExpired when the TTL has passed.
func (u *UUIDIssuer) Parse(token string) (*ParsedToken, error) {
	if u == nil || len(u.signingKey) == 0 {
		return nil, ErrTokenInvalid
	}
	if !strings.HasPrefix(token, "tok:") {
		return nil, ErrTokenInvalid
	}
	parts := strings.SplitN(token, ":", 5)
	if len(parts) != 5 {
		return nil, ErrTokenInvalid
	}
	id := parts[1]
	expiryNs, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, ErrTokenInvalid
	}
	payload := strings.Join(parts[:4], ":")
	expectedSig := u.sign(payload)
	if !hmac.Equal([]byte(expectedSig), []byte(parts[4])) {
		return nil, ErrTokenInvalid
	}
	subjectBytes, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return nil, ErrTokenInvalid
	}
	subject := string(subjectBytes)
	if id == "" || subject == "" {
		return nil, ErrTokenInvalid
	}
	if expiryNs != 0 && time.Now().UnixNano() > expiryNs {
		return nil, ErrTokenExpired
	}
	return &ParsedToken{ID: id, Subject: subject}, nil
}
