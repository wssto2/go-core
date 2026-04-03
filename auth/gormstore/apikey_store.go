package gormstore

import (
	"context"
	"errors"
	"time"

	"github.com/wssto2/go-core/auth"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// dummyAPIKeyHash is a pre-computed bcrypt hash used to perform a constant-time
// comparison when no DB candidates are found, preventing timing-based prefix enumeration.
var dummyAPIKeyHash, _ = bcrypt.GenerateFromPassword([]byte("$dummy-api-key$"), bcrypt.DefaultCost)

type gormAPIKeyStore struct {
	db *gorm.DB
}

func NewGormAPIKeyStore(db *gorm.DB) auth.APIKeyStore {
	return &gormAPIKeyStore{db: db}
}

func (s *gormAPIKeyStore) CreateKey(ctx context.Context, k *auth.APIKey, raw string) error {
	if len(raw) < 8 {
		return errors.New("api key too short")
	}

	hashed, err := (&auth.BcryptHasher{}).Hash(raw)
	if err != nil {
		return err
	}
	k.KeyPrefix = raw[:8]
	k.KeyHash = hashed
	k.CreatedAt = time.Now()
	return s.db.WithContext(ctx).Create(k).Error
}

func (s *gormAPIKeyStore) FindByKey(ctx context.Context, raw string) (*auth.APIKey, error) {
	if len(raw) < 8 {
		return nil, gorm.ErrRecordNotFound
	}
	prefix := raw[:8]

	var candidates []auth.APIKey
	if err := s.db.WithContext(ctx).
		Where("key_prefix = ? AND revoked = ?", prefix, false).
		Find(&candidates).Error; err != nil {
		return nil, err
	}

	// Perform a dummy comparison when no candidates exist to normalize timing
	// and prevent attackers from enumerating valid key prefixes.
	if len(candidates) == 0 {
		_ = bcrypt.CompareHashAndPassword(dummyAPIKeyHash, []byte(raw))
		return nil, gorm.ErrRecordNotFound
	}

	// Iterate ALL candidates before returning to avoid early-exit timing leaks.
	var found *auth.APIKey
	for _, c := range candidates {
		c := c
		if (&auth.BcryptHasher{}).Compare(raw, c.KeyHash) && found == nil {
			found = &c
		}
	}
	if found == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return found, nil
}

func (s *gormAPIKeyStore) RevokeKey(ctx context.Context, id int) error {
	return s.db.WithContext(ctx).Model(&auth.APIKey{}).Where("id = ?", id).Update("revoked", true).Error
}
