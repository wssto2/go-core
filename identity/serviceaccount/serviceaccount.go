// Package serviceaccount is the systems that only ever call the API, with API keys: a DMS export, a portal
// feed. A service account is a subject of the authorization engine like a person, given roles at a place
// through bindings (authz.KindServiceAccount), but it has no password and never signs in. identity.WithServiceAccounts
// installs the module: the tables, the administration service and routes under /v1/iam/service-accounts, the
// authentication of the X-API-Key header on identity's chain, the audit of what a key changes and the daily
// notices about keys that expire or go unused.
//
// This package is the core: the types, the Store port and the Service. The SQL store is gormstore, the routes
// are in http, the tables in migrations and the conformance suite of stores in storetest. A key is 256 random
// bits behind a recognisable prefix; only its SHA-256 is stored, so a leaked table holds no usable key.
package serviceaccount

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

// Errors of the Store.
var (
	// ErrNotFound: no such service account.
	ErrNotFound = errors.New("service account not found")
	// ErrKeyNotFound: no such API key.
	ErrKeyNotFound = errors.New("api key not found")
)

// Account is one system's identity. TenantID zero is the organization's account; with tenants, an account
// of a tenant can only be given roles within that tenant.
type Account struct {
	ID          int
	Name        string
	Description string
	// TenantID is the tenant the account is confined to, zero for the organization's.
	TenantID int
	// OwnerID is the person answerable for the account, and told when its keys expire or go unused.
	OwnerID       int
	CreatedBy     int
	CreatedAt     time.Time
	UpdatedBy     int
	UpdatedAt     *time.Time
	DeactivatedAt *time.Time
	DeactivatedBy int
}

// Active reports whether the account still works. A deactivated account's keys all stop working.
func (a Account) Active() bool { return a.DeactivatedAt == nil }

// Summary is an account with what the list shows of its keys.
type Summary struct {
	Account

	// ActiveKeys counts the keys that work now (not revoked, not expired).
	ActiveKeys int
	// LastUsedAt is the latest use of any of its keys.
	LastUsedAt *time.Time
}

// Key is one API key. The secret is never stored, only its hash; Prefix is the first characters of the
// secret, to tell keys apart.
type Key struct {
	ID               int
	ServiceAccountID int
	Name             string
	Prefix           string
	// Hash is the SHA-256 of the secret, hex. It is set when a key is created and never leaves the store
	// afterwards: it is not in the audit trail.
	Hash       string `audit:"-"`
	CreatedBy  int
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	RevokedBy  int
}

// Usable reports whether the key works at the moment: not revoked and not expired. Whether its account
// is still active is the account's to say.
func (k Key) Usable(at time.Time) bool {
	if k.RevokedAt != nil {
		return false
	}

	return k.ExpiresAt == nil || at.Before(*k.ExpiresAt)
}

// Visibility says which accounts a caller may list: all of them, or those of the tenants named.
type Visibility struct {
	All     bool
	Tenants []int
}

// Sees reports whether the visibility reaches the account.
func (v Visibility) Sees(a Account) bool {
	if v.All {
		return true
	}

	for _, id := range v.Tenants {
		if a.TenantID > 0 && id == a.TenantID {
			return true
		}
	}

	return false
}

// Store keeps service accounts and their keys. gormstore is the SQL one; storetest holds a store to it.
type Store interface {
	// List returns the accounts the visibility reaches, by name, with their key summary as of at.
	List(ctx context.Context, visible Visibility, at time.Time) ([]Summary, error)
	// Find returns one account with its key summary as of at, or ErrNotFound.
	Find(ctx context.Context, id int, at time.Time) (Summary, error)
	// Create stores the account and sets its ID.
	Create(ctx context.Context, account *Account) error
	// Update stores the name, description, tenant, owner, update and deactivation of the account.
	Update(ctx context.Context, account Account) error

	// Keys returns every key of an account, newest first.
	Keys(ctx context.Context, accountID int) ([]Key, error)
	// FindKey returns one key of the account, or ErrKeyNotFound.
	FindKey(ctx context.Context, accountID, keyID int) (Key, error)
	// CreateKey stores the key and sets its ID.
	CreateKey(ctx context.Context, key *Key) error
	// RevokeKey marks a key revoked; revoking a revoked key keeps the first revocation.
	RevokeKey(ctx context.Context, keyID int, at time.Time, by int) error

	// FindByKeyHash returns the key with the hash and its account, or ErrKeyNotFound. It is what
	// authentication asks on every API call.
	FindByKeyHash(ctx context.Context, hash string) (Key, Account, error)
	// TouchKey records a use of the key.
	TouchKey(ctx context.Context, keyID int, at time.Time) error

	NoticeStore
}

// DefaultKeyPrefix starts the secret of every key unless Config.KeyPrefix says otherwise, so a secret is
// recognised (and a leaked one found by a scanner) for what it is.
const DefaultKeyPrefix = "sak_"

// HashSecret is what is stored of a secret: its SHA-256, hex. The secrets are 256 random bits, so no slow
// hash is needed to make guessing them hard, and a plain hash lets the database find the key.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))

	return hex.EncodeToString(sum[:])
}

// PrefixOf is the part of a secret kept to tell keys apart: enough to recognise it, too little to use.
func PrefixOf(secret string) string {
	const shown = 12

	if len(secret) <= shown {
		return secret
	}

	return secret[:shown]
}

// Caller is the identity of a request made with an API key: it is what auth.GetIdentifiable and
// auth.UserOf[serviceaccount.Caller] return on such a request. It is not a person: GetID is 0, so an
// account's ID never passes for a user's, and the rate limit counts it under its own key.
type Caller struct {
	// AccountID is the service account, KeyID the key it presented.
	AccountID int
	KeyID     int
	Name      string
	TenantID  int
}

// GetID implements auth.Identifiable. A service account is not a user: it has no user ID.
func (Caller) GetID() int { return 0 }

// GetTenantID implements tenancy.TenantAware: the tenant the account is confined to.
func (c Caller) GetTenantID() int { return c.TenantID }

// HasTenant implements tenancy.TenantAware. The organization's account has no tenant, so it crosses them.
func (c Caller) HasTenant() bool { return c.TenantID > 0 }

// RateLimitKey implements auth.RateLimitKeyer: each service account has a bucket of its own.
func (c Caller) RateLimitKey() string { return "service:" + strconv.Itoa(c.AccountID) }
