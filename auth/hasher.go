package auth

import (
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
