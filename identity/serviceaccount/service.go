package serviceaccount

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/authz"
)

// MaxActiveKeys is how many keys an account may have working at once: enough to rotate one for another
// without a window in which the caller has none, few enough that nobody loses count.
const MaxActiveKeys = 10

// maxKeyLife is the furthest expiry a key may be given.
const maxKeyLife = 5 * 365 * 24 * time.Hour

// The widths of the columns, which the inputs' tags repeat.
const (
	NameMax        = 100
	DescriptionMax = 500
)

// Person is somebody by name.
type Person struct {
	ID   int
	Name string
}

// TenantRef is a tenant by name.
type TenantRef struct {
	ID   int
	Name string
}

// AccountView is a service account as the editors show it.
type AccountView struct {
	ID          int
	Name        string
	Description string
	// Tenant is nil for the organization's account.
	Tenant     *TenantRef
	Owner      Person
	CreatedBy  Person
	CreatedAt  time.Time
	ActiveKeys int
	LastUsedAt *time.Time
	// DeactivatedAt is set once the account was deactivated: its keys stop working.
	DeactivatedAt *time.Time
}

// KeyView is an API key as the editors show it; the secret is not part of it.
type KeyView struct {
	ID         int
	Name       string
	Prefix     string
	CreatedBy  Person
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
}

// CreatedKey is a new key and its secret, which is shown this once and never stored.
type CreatedKey struct {
	Key    KeyView
	Secret string
}

// SaveInput creates or updates a service account. TenantID is the tenant the account is confined to, or
// nothing for the organization's (the application has tenants only when Config.Tenants says so);
// OwnerID is the person answerable for it.
type SaveInput struct {
	Name        string `json:"name"        validation:"required|max:100"`
	Description string `json:"description" validation:"max:500"`
	TenantID    *int   `json:"tenant_id"`
	OwnerID     int    `json:"owner_id"    validation:"required|min:1"`
}

// KeyInput creates an API key. ExpiresAt, when given, is when the key stops working; nothing means it never does.
type KeyInput struct {
	Name      string     `json:"name"       validation:"required|max:100"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (i *SaveInput) sanitize() {
	i.Name = strings.TrimSpace(i.Name)
	i.Description = strings.TrimSpace(i.Description)

	if i.TenantID != nil && *i.TenantID <= 0 {
		i.TenantID = nil
	}
}

// Service administers service accounts and their keys, and knows which account a secret belongs to.
type Service struct {
	deps   Deps
	cfg    Config
	prefix string
}

// New returns the Service.
func New(deps Deps, cfg Config) *Service {
	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = DefaultKeyPrefix
	}

	return &Service{deps: deps, cfg: cfg, prefix: prefix}
}

func (s *Service) authorizer() (authz.Authorizer, error) {
	if s.deps.Authorizer != nil {
		if a := s.deps.Authorizer(); a != nil {
			return a, nil
		}
	}

	return nil, apperr.Internal(errNoAuthorizer)
}

var errNoAuthorizer = errors.New("serviceaccount: the application has no authorizer: install access, or pass gocore.WithAuthorizer")

// visibility is which accounts the actor may see: every one when they hold the view permission at a place
// above the tenants, otherwise those of the tenants they hold it at.
func (s *Service) visibility(ctx context.Context) (Visibility, error) {
	a, err := s.authorizer()
	if err != nil {
		return Visibility{}, err
	}

	if err := a.Require(ctx, View); err != nil {
		return Visibility{}, err
	}

	if s.cfg.Tenants == nil {
		return Visibility{All: true}, nil
	}

	set, err := a.Access(ctx, View)
	if err != nil {
		return Visibility{}, err
	}

	var out Visibility

	seen := map[int]bool{}

	for _, c := range set.Clauses {
		place, found := c.Chain.At(s.cfg.Tenants.Level)
		if !found {
			return Visibility{All: true}, nil
		}

		if !seen[place.ID] {
			seen[place.ID] = true
			out.Tenants = append(out.Tenants, place.ID)
		}
	}

	return out, nil
}

// Reach says whether the actor may see the service account at all (it answers access.SubjectReach for
// the accounts): false for one of another tenant, and for one that does not exist.
func (s *Service) Reach(ctx context.Context, id int) (bool, error) {
	if _, err := s.find(ctx, id); err != nil {
		if apperr.HasCode(err, apperr.CodeNotFound) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// Names returns the name of each service account among ids that exists (access.SubjectDirectory's).
func (s *Service) Names(ctx context.Context, ids []int) (map[int]string, error) {
	out := make(map[int]string, len(ids))

	for _, id := range ids {
		found, err := s.deps.Store.Find(ctx, id, s.deps.Clock.Now())
		if errors.Is(err, ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, apperr.Internal(fmt.Errorf("serviceaccount: read %d: %w", id, err))
		}

		out[id] = found.Name
	}

	return out, nil
}

// List returns the accounts the actor may see, by name.
func (s *Service) List(ctx context.Context) ([]AccountView, error) {
	v, err := s.visibility(ctx)
	if err != nil {
		return nil, err
	}

	found, err := s.deps.Store.List(ctx, v, s.deps.Clock.Now())
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("serviceaccount: list: %w", err))
	}

	return s.describe(ctx, found)
}

// Get returns one account, or a not-found for one the actor may not see.
func (s *Service) Get(ctx context.Context, id int) (AccountView, error) {
	found, err := s.find(ctx, id)
	if err != nil {
		return AccountView{}, err
	}

	return s.describeOne(ctx, found)
}

// find reads an account the actor may see: an account of another tenant is a not-found, not a refusal that
// admits it exists.
func (s *Service) find(ctx context.Context, id int) (Summary, error) {
	v, err := s.visibility(ctx)
	if err != nil {
		return Summary{}, err
	}

	found, err := s.deps.Store.Find(ctx, id, s.deps.Clock.Now())
	if errors.Is(err, ErrNotFound) {
		return Summary{}, apperr.NotFound("service account not found")
	}

	if err != nil {
		return Summary{}, apperr.Internal(fmt.Errorf("serviceaccount: read %d: %w", id, err))
	}

	if !v.Sees(found.Account) {
		return Summary{}, apperr.NotFound("service account not found")
	}

	return found, nil
}

// manager is the person who may change accounts: they hold the permission and are a person. A service
// account never mints credentials.
func (s *Service) manager(ctx context.Context, permission string) (authz.Principal, error) {
	a, err := s.authorizer()
	if err != nil {
		return authz.Principal{}, err
	}

	if err := a.Require(ctx, permission); err != nil {
		return authz.Principal{}, err
	}

	actor, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return authz.Principal{}, apperr.Unauthorized("not authenticated")
	}

	if actor.Kind != authz.KindUser {
		return authz.Principal{}, apperr.ForbiddenErr(errPersonOnly).
			WithReason(authz.ReasonForbidden, map[string]any{"why": "person_only"})
	}

	return actor, nil
}

var errPersonOnly = errors.New("serviceaccount: only a person manages service accounts")

// Create makes an account. Nothing works on it until it is given roles and a key.
func (s *Service) Create(ctx context.Context, in SaveInput) (AccountView, error) {
	in.sanitize()

	if err := s.check(in); err != nil {
		return AccountView{}, err
	}

	actor, err := s.manager(ctx, Manage)
	if err != nil {
		return AccountView{}, err
	}

	if err := s.checkPlaces(ctx, 0, in); err != nil {
		return AccountView{}, err
	}

	account := Account{
		Name: in.Name, Description: in.Description, TenantID: tenantOf(in), OwnerID: in.OwnerID,
		CreatedBy: actor.ID, CreatedAt: s.deps.Clock.Now(),
	}

	err = s.deps.Transact.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.deps.Store.Create(ctx, &account); err != nil {
			return fmt.Errorf("create: %w", err)
		}

		return s.deps.Trail.Write(ctx, audit.NewEntry(AccountEntity, account.ID, actor.ID, "create").WithAfter(account))
	})
	if err != nil {
		return AccountView{}, apperr.Internal(fmt.Errorf("serviceaccount: create: %w", err))
	}

	return s.Get(ctx, account.ID)
}

// Update changes an account's name, description, owner and tenant.
func (s *Service) Update(ctx context.Context, id int, in SaveInput) (AccountView, error) {
	in.sanitize()

	if err := s.check(in); err != nil {
		return AccountView{}, err
	}

	actor, err := s.manager(ctx, Manage)
	if err != nil {
		return AccountView{}, err
	}

	found, err := s.find(ctx, id)
	if err != nil {
		return AccountView{}, err
	}

	if err := s.checkPlaces(ctx, id, in); err != nil {
		return AccountView{}, err
	}

	before, after := found.Account, found.Account
	after.Name, after.Description, after.TenantID, after.OwnerID = in.Name, in.Description, tenantOf(in), in.OwnerID
	after.UpdatedBy = actor.ID
	now := s.deps.Clock.Now()
	after.UpdatedAt = &now

	err = s.deps.Transact.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.deps.Store.Update(ctx, after); err != nil {
			return fmt.Errorf("update: %w", err)
		}

		entry := audit.NewEntry(AccountEntity, id, actor.ID, "update").WithBefore(before).WithAfter(after).WithDiff(audit.Diff(before, after))

		return s.deps.Trail.Write(ctx, entry)
	})
	if err != nil {
		return AccountView{}, apperr.Internal(fmt.Errorf("serviceaccount: update: %w", err))
	}

	return s.Get(ctx, id)
}

// The audit trail's record types and the actions it holds.
const (
	// AccountEntity is the record type of an account in the audit trail; KeyEntity of a key.
	AccountEntity = "service_accounts"
	KeyEntity     = "service_account_keys"
)

// check validates the input the way the route's tags do, for code that calls the Service.
func (s *Service) check(in SaveInput) error {
	failures := map[string]string{}

	switch {
	case in.Name == "":
		failures["name"] = "required"
	case utf8.RuneCountInString(in.Name) > NameMax:
		failures["name"] = "max"
	}

	if utf8.RuneCountInString(in.Description) > DescriptionMax {
		failures["description"] = "max"
	}

	if in.OwnerID < 1 {
		failures["owner_id"] = "required"
	}

	if len(failures) > 0 {
		return apperr.BadRequest(fmt.Sprintf("invalid service account: %v", failures))
	}

	return nil
}

// checkPlaces refuses a tenant or an owner that does not exist, and a tenant the application refuses.
func (s *Service) checkPlaces(ctx context.Context, accountID int, in SaveInput) error {
	if in.TenantID != nil {
		t := s.cfg.Tenants
		if t == nil {
			return apperr.BadRequest("tenant_id: this application has no tenants")
		}

		names, err := t.Names(ctx, []int{*in.TenantID})
		if err != nil {
			return apperr.Internal(fmt.Errorf("serviceaccount: tenant: %w", err))
		}

		if _, ok := names[*in.TenantID]; !ok {
			return apperr.BadRequest("tenant_id: no such tenant")
		}

		if t.Confine != nil {
			if err := t.Confine(ctx, accountID, *in.TenantID); err != nil {
				return err
			}
		}
	}

	people, err := s.deps.People.Names(ctx, []int{in.OwnerID})
	if err != nil {
		return apperr.Internal(fmt.Errorf("serviceaccount: owner: %w", err))
	}

	if _, ok := people[in.OwnerID]; !ok {
		return apperr.BadRequest("owner_id: no such person")
	}

	return nil
}

func tenantOf(in SaveInput) int {
	if in.TenantID == nil {
		return 0
	}

	return *in.TenantID
}

// Deactivate stops the account: every key of it stops working at once, the roles it was given stay so that it
// can be looked at. Deactivating twice keeps the first.
func (s *Service) Deactivate(ctx context.Context, id int) (AccountView, error) {
	actor, err := s.manager(ctx, Manage)
	if err != nil {
		return AccountView{}, err
	}

	found, err := s.find(ctx, id)
	if err != nil {
		return AccountView{}, err
	}

	if !found.Active() {
		return s.describeOne(ctx, found)
	}

	before, after := found.Account, found.Account
	now := s.deps.Clock.Now()
	after.DeactivatedAt, after.DeactivatedBy = &now, actor.ID
	after.UpdatedBy, after.UpdatedAt = actor.ID, &now

	err = s.deps.Transact.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.deps.Store.Update(ctx, after); err != nil {
			return fmt.Errorf("deactivate: %w", err)
		}

		entry := audit.NewEntry(AccountEntity, id, actor.ID, "deactivate").WithBefore(before).WithAfter(after).WithDiff(audit.Diff(before, after))

		return s.deps.Trail.Write(ctx, entry)
	})
	if err != nil {
		return AccountView{}, apperr.Internal(fmt.Errorf("serviceaccount: deactivate: %w", err))
	}

	return s.Get(ctx, id)
}

// Keys lists an account's keys, newest first.
func (s *Service) Keys(ctx context.Context, id int) ([]KeyView, error) {
	if _, err := s.find(ctx, id); err != nil {
		return nil, err
	}

	keys, err := s.deps.Store.Keys(ctx, id)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("serviceaccount: keys of %d: %w", id, err))
	}

	return s.describeKeys(ctx, keys)
}

// CreateKey makes a key and returns its secret, once. Only the hash is stored.
func (s *Service) CreateKey(ctx context.Context, id int, in KeyInput) (CreatedKey, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > NameMax {
		return CreatedKey{}, apperr.BadRequest("name: required, at most 100 characters")
	}

	actor, err := s.manager(ctx, Manage)
	if err != nil {
		return CreatedKey{}, err
	}

	found, err := s.find(ctx, id)
	if err != nil {
		return CreatedKey{}, err
	}

	if !found.Active() {
		return CreatedKey{}, apperr.BadRequest("the service account is deactivated")
	}

	now := s.deps.Clock.Now()

	var expires *time.Time

	if in.ExpiresAt != nil && !in.ExpiresAt.IsZero() {
		at := in.ExpiresAt.UTC()
		if !at.After(now) || at.After(now.Add(maxKeyLife)) {
			return CreatedKey{}, apperr.BadRequest("expires_at must be in the future, within five years")
		}

		expires = &at
	}

	if found.ActiveKeys >= MaxActiveKeys {
		return CreatedKey{}, apperr.BadRequest(fmt.Sprintf("at most %d keys work at once; revoke one first", MaxActiveKeys))
	}

	secret, err := s.newSecret()
	if err != nil {
		return CreatedKey{}, err
	}

	key := Key{
		ServiceAccountID: id, Name: in.Name, Prefix: PrefixOf(secret), Hash: HashSecret(secret),
		CreatedBy: actor.ID, CreatedAt: now, ExpiresAt: expires,
	}

	err = s.deps.Transact.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.deps.Store.CreateKey(ctx, &key); err != nil {
			return fmt.Errorf("create key: %w", err)
		}

		// the trail never holds the hash, let alone the secret
		trailed := key
		trailed.Hash = ""

		return s.deps.Trail.Write(ctx, audit.NewEntry(KeyEntity, key.ID, actor.ID, "create").WithAfter(trailed))
	})
	if err != nil {
		return CreatedKey{}, apperr.Internal(fmt.Errorf("serviceaccount: create key: %w", err))
	}

	views, err := s.describeKeys(ctx, []Key{key})
	if err != nil {
		return CreatedKey{}, err
	}

	return CreatedKey{Key: views[0], Secret: secret}, nil
}

// RevokeKey stops one key for good. Revoking a revoked key is not an error.
func (s *Service) RevokeKey(ctx context.Context, id, keyID int) error {
	a, err := s.authorizer()
	if err != nil {
		return err
	}

	if err := a.Require(ctx, Revoke); err != nil {
		return err
	}

	actor, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return apperr.Unauthorized("not authenticated")
	}

	if _, err := s.find(ctx, id); err != nil {
		return err
	}

	key, err := s.deps.Store.FindKey(ctx, id, keyID)
	if errors.Is(err, ErrKeyNotFound) {
		return apperr.NotFound("key not found")
	}

	if err != nil {
		return apperr.Internal(fmt.Errorf("serviceaccount: read key %d: %w", keyID, err))
	}

	if key.RevokedAt != nil {
		return nil
	}

	by := 0
	if actor.Kind == authz.KindUser {
		by = actor.ID
	}

	err = s.deps.Transact.WithinTransaction(ctx, func(ctx context.Context) error {
		now := s.deps.Clock.Now()
		if err := s.deps.Store.RevokeKey(ctx, keyID, now, by); err != nil {
			return fmt.Errorf("revoke: %w", err)
		}

		after := key
		after.RevokedAt, after.RevokedBy = &now, by

		return s.deps.Trail.Write(ctx, audit.NewEntry(KeyEntity, keyID, by, "revoke").WithBefore(key).WithAfter(after))
	})
	if err != nil {
		return apperr.Internal(fmt.Errorf("serviceaccount: revoke: %w", err))
	}

	return nil
}

// Authenticate finds the account a secret belongs to, and the key. Every reason for a refusal looks the same
// to the caller (401), and nothing is cached, so a revoked key or a deactivated account stops working at
// once. A use is recorded as the key's last use.
func (s *Service) Authenticate(ctx context.Context, secret string) (Caller, error) {
	unauthorized := apperr.Unauthorized("invalid API key")

	const maxSecret = 256 // a secret is 48 characters: do not hash what cannot be one

	if secret == "" || len(secret) > maxSecret {
		return Caller{}, unauthorized
	}

	key, account, err := s.deps.Store.FindByKeyHash(ctx, HashSecret(secret))
	if errors.Is(err, ErrKeyNotFound) {
		return Caller{}, unauthorized
	}

	if err != nil {
		return Caller{}, apperr.Internal(fmt.Errorf("serviceaccount: authenticate: %w", err))
	}

	now := s.deps.Clock.Now()
	if !key.Usable(now) || !account.Active() {
		return Caller{}, unauthorized
	}

	// The last use is a courtesy to whoever answers for the key: recorded at most once a minute, and a failure to
	// record it never fails the call.
	if key.LastUsedAt == nil || now.Sub(*key.LastUsedAt) >= time.Minute {
		_ = s.deps.Store.TouchKey(ctx, key.ID, now) // best effort, see above
	}

	return Caller{AccountID: account.ID, KeyID: key.ID, Name: account.Name, TenantID: account.TenantID}, nil
}

// newSecret is a new API key secret: 256 random bits behind the recognisable prefix.
func (s *Service) newSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", apperr.Internal(fmt.Errorf("serviceaccount: secret: %w", err))
	}

	return s.prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// describe names the tenants and the people of the accounts, in one lookup each.
func (s *Service) describe(ctx context.Context, found []Summary) ([]AccountView, error) {
	tenantIDs := make([]int, 0, len(found))
	personIDs := make([]int, 0, 2*len(found))

	for _, f := range found {
		if f.TenantID > 0 {
			tenantIDs = append(tenantIDs, f.TenantID)
		}

		personIDs = append(personIDs, f.OwnerID, f.CreatedBy)
	}

	var tenants map[int]string

	if len(tenantIDs) > 0 && s.cfg.Tenants != nil {
		var err error
		if tenants, err = s.cfg.Tenants.Names(ctx, tenantIDs); err != nil {
			return nil, apperr.Internal(fmt.Errorf("serviceaccount: tenant names: %w", err))
		}
	}

	people, err := s.deps.People.Names(ctx, personIDs)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("serviceaccount: people names: %w", err))
	}

	out := make([]AccountView, 0, len(found))

	for _, f := range found {
		view := AccountView{
			ID: f.ID, Name: f.Name, Description: f.Description,
			Owner:     Person{ID: f.OwnerID, Name: people[f.OwnerID]},
			CreatedBy: Person{ID: f.CreatedBy, Name: people[f.CreatedBy]},
			CreatedAt: f.CreatedAt, ActiveKeys: f.ActiveKeys, LastUsedAt: f.LastUsedAt, DeactivatedAt: f.DeactivatedAt,
		}

		if f.TenantID > 0 {
			view.Tenant = &TenantRef{ID: f.TenantID, Name: tenants[f.TenantID]}
		}

		out = append(out, view)
	}

	return out, nil
}

func (s *Service) describeOne(ctx context.Context, found Summary) (AccountView, error) {
	out, err := s.describe(ctx, []Summary{found})
	if err != nil {
		return AccountView{}, err
	}

	return out[0], nil
}

func (s *Service) describeKeys(ctx context.Context, keys []Key) ([]KeyView, error) {
	ids := make([]int, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.CreatedBy)
	}

	people, err := s.deps.People.Names(ctx, ids)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("serviceaccount: people names: %w", err))
	}

	out := make([]KeyView, 0, len(keys))
	for _, k := range keys {
		out = append(out, KeyView{
			ID: k.ID, Name: k.Name, Prefix: k.Prefix, CreatedBy: Person{ID: k.CreatedBy, Name: people[k.CreatedBy]},
			CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt, ExpiresAt: k.ExpiresAt, RevokedAt: k.RevokedAt,
		})
	}

	return out, nil
}
