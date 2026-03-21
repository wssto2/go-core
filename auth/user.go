package auth

import (
	"slices"

	"golang.org/x/crypto/bcrypt"
)

// Identifiable is the interface for any entity that can be authenticated.
type Identifiable interface {
	GetID() int
	GetEmail() string
	GetPolicies() []string
	HasPolicy(policy string) bool
	HasAnyPolicy(policy ...string) bool
	HasAllPolicies(policy ...string) bool
}

// User is a generic authenticated user.
type User[T any] struct {
	ID       int      `json:"id" gorm:"primaryKey"`
	Email    string   `json:"email"`
	Policies []string `json:"policies" gorm:"-"`
	Data     T        `json:"data,omitempty" gorm:"-"` // Application-specific user data (e.g. Dealer, Preferences)
}

func (u *User[T]) GetID() int {
	return u.ID
}

func (u *User[T]) GetEmail() string {
	return u.Email
}

func (u *User[T]) GetPolicies() []string {
	return u.Policies
}

func (u *User[T]) HasPolicy(policy string) bool {
	return slices.Contains(u.Policies, policy)
}

func (u *User[T]) HasAnyPolicy(policy ...string) bool {
	return slices.ContainsFunc(policy, u.HasPolicy)
}

func (u *User[T]) HasAllPolicies(policy ...string) bool {
	for _, p := range policy {
		if !u.HasPolicy(p) {
			return false
		}
	}
	return true
}

// --- Pluggable Password Hashing ---

// Hasher defines the interface for password hashing strategies.
type Hasher interface {
	Hash(password string) (string, error)
	Compare(password, hash string) bool
}

// DefaultHasher is used by the HashPassword and CheckPasswordHash helpers.
// It defaults to BcryptHasher.
var DefaultHasher Hasher = &BcryptHasher{}

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
	return DefaultHasher.Hash(password)
}

// CheckPasswordHash compares a hashed password with its plaintext equivalent using the DefaultHasher.
func CheckPasswordHash(password, hash string) bool {
	return DefaultHasher.Compare(password, hash)
}
