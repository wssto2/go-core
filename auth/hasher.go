package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"

	"golang.org/x/crypto/bcrypt"
)

// Hasher defines the interface for password hashing strategies.
type Hasher interface {
	Hash(password string) (string, error)
	Compare(password, hash string) bool
}

// BcryptHasher implements Hasher using industry-standard bcrypt.
type BcryptHasher struct {
	Cost int
}

// NewBcryptHasher creates a new BcryptHasher with the given cost.
func NewBcryptHasher(cost int) *BcryptHasher {
	return &BcryptHasher{Cost: cost}
}

// Hash hashes the given password using bcrypt with the configured cost.
func (b *BcryptHasher) Hash(password string) (string, error) {
	cost := b.Cost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}

	bytes, err := bcrypt.GenerateFromPassword([]byte(password), cost)

	return string(bytes), err
}

// Compare compares the given password with the stored hash using bcrypt.
func (b *BcryptHasher) Compare(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// HMACHasher implements Hasher using HMAC-SHA256. It is orders of magnitude
// faster than bcrypt and appropriate for high-entropy random tokens (e.g.
// refresh tokens) where hash-slowness provides no security benefit.
type HMACHasher struct {
	Key []byte
}

// NewHMACHasher creates a new HMACHasher with the given secret key.
func NewHMACHasher(key []byte) *HMACHasher {
	return &HMACHasher{Key: key}
}

// Hash computes HMAC-SHA256 of the token and returns a base64url-encoded string.
func (h *HMACHasher) Hash(token string) (string, error) {
	mac := hmac.New(sha256.New, h.Key)
	mac.Write([]byte(token))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Compare uses a constant-time comparison to prevent timing attacks.
func (h *HMACHasher) Compare(token, hash string) bool {
	expected, err := h.Hash(token)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(expected), []byte(hash))
}
