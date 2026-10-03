package account

import "golang.org/x/crypto/bcrypt"

// Bcrypt is the default PasswordHasher. The zero value uses bcrypt's default cost.
type Bcrypt struct{ Cost int }

// Hash implements PasswordHasher.
func (b Bcrypt) Hash(password string) (string, error) {
	cost := b.Cost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}

	out, err := bcrypt.GenerateFromPassword([]byte(password), cost)

	return string(out), err
}

// Matches implements PasswordHasher.
func (Bcrypt) Matches(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
