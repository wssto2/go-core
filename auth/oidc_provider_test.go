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
	"sync"
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

func TestOIDCProvider_ClientDisconnect_DoesNotCancelOtherRequests(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pub := &priv.PublicKey
	kid := "key-disconnect-test"

	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	eBytes := big.NewInt(int64(pub.E)).Bytes()
	e := base64.RawURLEncoding.EncodeToString(eBytes)
	jwksPayload := map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256", "n": n, "e": e,
	}}}
	jwksBytes, err := json.Marshal(jwksPayload)
	require.NoError(t, err)

	var fetchCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetchCount, 1)
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksBytes)
	}))
	defer server.Close()

	resolver := func(_ context.Context, _ string) (auth.Identifiable, error) {
		return &testUser{id: 42}, nil
	}

	// Short TTL so the cache is expired when we trigger the second fetch.
	p := auth.NewOIDCProvider(server.URL, "test-issuer", resolver, 1*time.Millisecond)

	buildToken := func() string {
		claims := auth.Claims{}
		claims.Issuer = "test-issuer"
		claims.Subject = "42"
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = kid
		signed, _ := tok.SignedString(priv)
		return signed
	}

	// First verify to warm the cache.
	_, err = p.Verify(context.Background(), buildToken())
	require.NoError(t, err)

	// Let the cache expire.
	time.Sleep(5 * time.Millisecond)

	// Launch two concurrent verifies. The first uses a cancelled context to
	// simulate a client disconnect. The second should still succeed because the
	// singleflight internally uses context.Background().
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	var wg sync.WaitGroup
	var secondErr error
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = p.Verify(cancelledCtx, buildToken()) // may fail, that's fine
	}()
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond) // join the in-flight request
		_, secondErr = p.Verify(context.Background(), buildToken())
	}()
	wg.Wait()

	// The second request MUST succeed even though the first had a cancelled ctx.
	require.NoError(t, secondErr, "second request must not fail due to client disconnect")
}
