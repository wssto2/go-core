package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/auth"
)

type fakeKeyStore struct{ key *auth.APIKey }

func (f fakeKeyStore) FindByKey(context.Context, string) (*auth.APIKey, error) { return f.key, nil }
func (fakeKeyStore) CreateKey(context.Context, *auth.APIKey, string) error     { return nil }
func (fakeKeyStore) RevokeKey(context.Context, int) error                      { return nil }

func TestValidateAPIKeyHonoursExpiry(t *testing.T) {
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	tests := []struct {
		name string
		key  auth.APIKey
		ok   bool
	}{
		{"no expiry", auth.APIKey{ID: 1}, true},
		{"not yet expired", auth.APIKey{ID: 1, ExpiresAt: &future}, true},
		{"expired", auth.APIKey{ID: 1, ExpiresAt: &past}, false},
		{"revoked", auth.APIKey{ID: 1, Revoked: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := auth.ValidateAPIKey(context.Background(), fakeKeyStore{&tt.key}, "raw-key-value")
			if tt.ok {
				require.NoError(t, err)
				assert.Equal(t, 1, got.ID)
				return
			}
			assert.ErrorIs(t, err, auth.ErrUnauthorized)
		})
	}
}
