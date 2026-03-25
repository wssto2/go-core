package auth

import (
	"golang.org/x/crypto/bcrypt"
)

// --- Pluggable Password Hashing ---

// Hasher defines the interface for password hashing strategies.
type Hasher interface {
	Hash(password string) (string, error)
	Compare(password, hash string) bool
}

// DefaultHasher is used by the HashPassword and CheckPasswordHash helpers.
// It defaults to BcryptHasher.
var defaultHasher Hasher = &BcryptHasher{}

// BcryptHasher implements Hasher using industry-standard bcrypt.
type BcryptHasher struct {
	Cost int
}

func (b *BcryptHasher) Hash(password string) (string, error) {
	cost := b.Cost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	return string(bytes), err
}

func (b *BcryptHasher) Compare(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// HashPassword creates a hash of the password using the DefaultHasher.
func HashPassword(password string) (string, error) {
	return defaultHasher.Hash(password)
}

// CheckPasswordHash compares a hashed password with its plaintext equivalent using the DefaultHasher.
func CheckPasswordHash(password, hash string) bool {
	return defaultHasher.Compare(password, hash)
}
