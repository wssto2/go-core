package serviceaccount

import (
	"context"
	"time"

	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/authz"
)

// The permissions of the service account routes. identity.DefinePermissions adds them to the catalogue.
const (
	// View reads service accounts, their keys and access. Whoever holds it at a tenant sees the accounts of
	// that tenant; at the root, all of them.
	View = "iam.serviceaccount:view"
	// Manage creates, edits and deactivates a service account, gives it access and creates its keys.
	Manage = "iam.serviceaccount:manage"
	// Revoke revokes a key. It is its own permission, so a tenant's administrator can stop a key they cannot make.
	Revoke = "iam.serviceaccount:revoke"
)

// Config tunes the module: both fields are optional.
type Config struct {
	// KeyPrefix starts the secret of every new key; DefaultKeyPrefix if empty. A key is authenticated
	// whatever its prefix, so keys made under another prefix keep working.
	KeyPrefix string
	// Tenants confines accounts to the tenants of an application whose data is scoped by tenant; nil means
	// no tenants: an account has none, and whoever may view accounts sees all of them.
	Tenants *Tenants
}

// Tenants says what an application's tenants are, so an account can be confined to one and an
// administrator sees the accounts of their own.
type Tenants struct {
	// Level is the authz hierarchy level whose places are the tenants, such as "dealer": whoever holds the
	// view permission at a place of that level sees the accounts of that tenant; at a higher place, all.
	Level string
	// Names returns the name of each tenant among ids that exists. A tenant missing from the result does
	// not exist, and an account cannot be confined to it.
	Names func(ctx context.Context, ids []int) (map[int]string, error)
	// Confine, when set, is asked before an account is confined to a tenant it was not in (on create and
	// update): return an error, built with apperr, to refuse, for example when the account holds a role at
	// another tenant's place.
	Confine func(ctx context.Context, accountID, tenantID int) error
}

// People names the persons who answer for accounts, and made them: identity's Users is one.
type People interface {
	// Names returns the display name of each person among ids that exists.
	Names(ctx context.Context, ids []int) (map[int]string, error)
}

// Transactor runs fn in one database transaction: database.Transactor is one.
type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock tells the time.
type Clock interface{ Now() time.Time }

// Trail records what changed: audit.Repository is one.
type Trail interface {
	Write(ctx context.Context, entry audit.Entry) error
}

// Deps are the collaborators of the Service.
type Deps struct {
	Store    Store
	Transact Transactor
	Trail    Trail
	Clock    Clock
	People   People
	// Authorizer is read when a request comes, because the engine is built after identity (access.Install).
	Authorizer func() authz.Authorizer
}
