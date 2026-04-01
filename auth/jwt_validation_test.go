package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIssueAndParseToken_WithAudienceAndIssuer(t *testing.T) {
	cfg := TokenConfig{
		SecretKey:     "secret",
		Issuer:        "my-issuer",
		Audience:      "my-aud",
		TokenDuration: time.Hour,
	}

	claims := Claims{UserID: 1, Email: "a@x.com"}
	tok, err := IssueToken(claims, cfg)
	assert.NoError(t, err)

	parsed, err := ParseToken(tok, cfg)
	assert.NoError(t, err)
	assert.Equal(t, "my-issuer", parsed.Issuer)
	// VerifyAudience helper should pass
	assert.Contains(t, parsed.Audience, "my-aud")
}

func TestParseToken_InvalidAudience(t *testing.T) {
	cfg1 := TokenConfig{SecretKey: "secret", Issuer: "iss", Audience: "aud1", TokenDuration: time.Hour}
	claims := Claims{UserID: 2}
	tok, err := IssueToken(claims, cfg1)
	assert.NoError(t, err)

	cfg2 := TokenConfig{SecretKey: "secret", Issuer: "iss", Audience: "aud2", TokenDuration: time.Hour}
	_, err = ParseToken(tok, cfg2)
	assert.ErrorIs(t, err, ErrInvalidClaims)
}

func TestParseToken_Expired(t *testing.T) {
	cfg := TokenConfig{SecretKey: "secret", Issuer: "iss", TokenDuration: -time.Hour}
	claims := Claims{UserID: 3}
	tok, err := IssueToken(claims, cfg)
	assert.NoError(t, err)

	_, err = ParseToken(tok, cfg)
	assert.ErrorIs(t, err, ErrExpiredToken)
}
