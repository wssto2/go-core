package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/wssto2/go-core/auth"
)

// tiny test user type
type testUser struct{ id int }

func (u *testUser) GetID() int { return u.id }

func TestOIDCProvider_VerifyAndCaching(t *testing.T) {
	// Generate RSA keypair
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pub := &priv.PublicKey

	kid := "test-kid"

	// build a JWKS response
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	eBytes := big.NewInt(int64(pub.E)).Bytes()
	e := base64.RawURLEncoding.EncodeToString(eBytes)

	jwks := map[string]any{"keys": []map[string]string{{
		"kty": "RSA",
		"kid": kid,
		"use": "sig",
		"alg": "RS256",
		"n":   n,
		"e":   e,
	}}}

	jwksBytes, err := json.Marshal(jwks)
	require.NoError(t, err)

	var called int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksBytes)
	}))
	defer server.Close()

	// resolver: convert subject to testUser
	resolver := func(ctx context.Context, id string) (auth.Identifiable, error) {
		// for test simplicity, ignore conversion errors
		return &testUser{id: 42}, nil
	}

	p := auth.NewOIDCProvider(server.URL, "test-issuer", resolver, 5*time.Minute)

	// create token signed with our private key and with kid header
	claims := auth.Claims{}
	claims.Issuer = "test-issuer"
	claims.Subject = "42"

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid

	signed, err := tok.SignedString(priv)
	require.NoError(t, err)

	// Verify first time - should fetch JWKS once
	user, err := p.Verify(context.Background(), signed)
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, 42, user.GetID())
	require.Equal(t, int32(1), atomic.LoadInt32(&called), "jwks should have been fetched once")

	// Verify again - should use cache (no new fetch)
	user2, err := p.Verify(context.Background(), signed)
	require.NoError(t, err)
	require.NotNil(t, user2)
	require.Equal(t, 42, user2.GetID())
	require.Equal(t, int32(1), atomic.LoadInt32(&called), "jwks should still be cached, no extra fetch")
}
