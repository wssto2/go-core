package push

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"
)

// ErrInvalidKeys means the VAPID key pair is unusable.
var ErrInvalidKeys = errors.New("push: invalid VAPID keys")

// vapidLifetime is how long a signed token is valid: RFC 8292 allows at most 24 hours.
const vapidLifetime = 12 * time.Hour

var b64 = base64.RawURLEncoding

// GenerateKeys makes a VAPID key pair, as base64url without padding: the public key is the 65-byte uncompressed point
// the browser subscribes with, the private key the 32-byte scalar. Keep the private key secret and stable: a new pair
// invalidates every subscription made with the old public key.
func GenerateKeys() (public, private string, err error) {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("push: generate the VAPID keys: %w", err)
	}

	return b64.EncodeToString(key.PublicKey().Bytes()), b64.EncodeToString(key.Bytes()), nil
}

// signer signs the VAPID tokens of one key pair.
type signer struct {
	key     *ecdsa.PrivateKey
	public  string
	subject string
}

// newSigner checks the pair, so a mismatch fails at start-up and not on every push.
func newSigner(public, private, subject string) (*signer, error) {
	scalar, err := decode(private)
	if err != nil || len(scalar) != 32 {
		return nil, fmt.Errorf("%w: the private key must be 32 bytes, base64url", ErrInvalidKeys)
	}

	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidKeys, err)
	}

	pub, err := decode(public)
	if err != nil || len(pub) != 65 {
		return nil, fmt.Errorf("%w: the public key must be a 65-byte uncompressed P-256 point, base64url", ErrInvalidKeys)
	}

	if own, err := key.PublicKey.Bytes(); err != nil || string(own) != string(pub) {
		return nil, fmt.Errorf("%w: the public key does not belong to the private key", ErrInvalidKeys)
	}

	sub, err := normalizeSubject(subject)
	if err != nil {
		return nil, err
	}

	return &signer{key: key, public: b64.EncodeToString(pub), subject: sub}, nil
}

// normalizeSubject keeps an https URL and makes an e-mail address a mailto: URI, as RFC 8292 wants.
func normalizeSubject(subject string) (string, error) {
	subject = strings.TrimSpace(subject)

	if u, err := url.Parse(subject); err == nil && u.Scheme == "https" && u.Host != "" {
		return subject, nil
	}

	subject = strings.TrimPrefix(subject, "mailto:")
	if !strings.Contains(subject, "@") {
		return "", fmt.Errorf("%w: the subject must be an https URL or an e-mail address", ErrInvalidKeys)
	}

	return "mailto:" + subject, nil
}

// authorization is the Authorization header of a push to endpoint (RFC 8292): a signed token for the endpoint's origin
// and the public key it can be checked with.
func (s *signer) authorization(endpoint string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("%w: the endpoint is not a URL", ErrSubscriptionUnusable)
	}

	aud := u.Scheme + "://" + u.Host

	claims, err := json.Marshal(map[string]any{"aud": aud, "exp": now.Add(vapidLifetime).Unix(), "sub": s.subject})
	if err != nil {
		return "", fmt.Errorf("push: encode the VAPID claims: %w", err)
	}

	signing := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`)) + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))

	der, err := ecdsa.SignASN1(rand.Reader, s.key, digest[:])
	if err != nil {
		return "", fmt.Errorf("push: sign the VAPID token: %w", err)
	}

	var rs struct{ R, S *big.Int }
	if _, err := asn1.Unmarshal(der, &rs); err != nil {
		return "", fmt.Errorf("push: read the signature: %w", err)
	}

	// JWS wants r and s as two fixed 32-byte numbers, not DER.
	sig := make([]byte, 64)
	rs.R.FillBytes(sig[:32])
	rs.S.FillBytes(sig[32:])

	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ", k=" + s.public, nil
}

// decode reads base64url, or standard base64, padded or not: what a browser or an operator may paste.
func decode(value string) ([]byte, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "=")

	out, err := b64.DecodeString(value)
	if err != nil {
		out, err = base64.RawStdEncoding.DecodeString(value)
	}

	return out, err //nolint:wrapcheck // callers only test for failure
}
