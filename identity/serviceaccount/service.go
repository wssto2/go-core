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
	// NameMax is the width of an account's and a key's name.
	NameMax = 100
	// DescriptionMax is the width of an account's description.
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
	TenantID    *int   `json:"tenant_id"`                               //nolint:tagliatelle // snake_case wire
	OwnerID     int    `json:"owner_id"    validation:"required|min:1"` //nolint:tagliatelle // snake_case wire
}

// KeyInput creates an API key. ExpiresAt, when given, is when the key stops working; nothing means it never does.
type KeyInput struct {
	Name      string     `json:"name"       validation:"required|max:100"`
	ExpiresAt *time.Time `json:"expires_at"` //nolint:tagliatelle // snake_case wire
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

var (
	errNoAuthorizer = errors.New(
		"serviceaccount: the application has no authorizer: install access, or pass gocore.WithAuthorizer")
	errPersonOnly = errors.New("serviceaccount: only a person manages service accounts")
)

// The audit trail's record types and the actions it holds.
const (
	// AccountEntity is the record type of an account in the audit trail; KeyEntity of a key.
	AccountEntity = "service_accounts"
	KeyEntity     = "service_account_keys"
)

const (
	// secretBytes is the size of a secret: 256 random bits.
	secretBytes = 32
	// personsPerAccount is the people an account names: its owner and its creator.
	personsPerAccount = 2
)

// Reach says whether the actor may see the service account at all (it answers admin.SubjectReach for
// the accounts): false for one of another tenant, and for one that does not exist.
func (s *Service) Reach(ctx context.Context, accountID int) (bool, error) {
	_, err := s.find(ctx, accountID)
	if err != nil {
		if apperr.HasCode(err, apperr.CodeNotFound) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// Names returns the name of each service account among ids that exists (admin.SubjectDirectory's).
func (s *Service) Names(ctx context.Context, ids []int) (map[int]string, error) {
	out := make(map[int]string, len(ids))

	for _, accountID := range ids {
		found, err := s.deps.Store.Find(ctx, accountID, s.deps.Clock.Now())
		if errors.Is(err, ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, apperr.Internal(fmt.Errorf("serviceaccount: read %d: %w", accountID, err))
		}

		out[accountID] = found.Name
	}

	return out, nil
}

// List returns the accounts the actor may see, by name.
func (s *Service) List(ctx context.Context) ([]AccountView, error) {
	visible, err := s.visibility(ctx)
	if err != nil {
		return nil, err
	}

	found, err := s.deps.Store.List(ctx, visible, s.deps.Clock.Now())
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("serviceaccount: list: %w", err))
	}

	return s.describe(ctx, found)
}

// Get returns one account, or a not-found for one the actor may not see.
func (s *Service) Get(ctx context.Context, accountID int) (AccountView, error) {
	found, err := s.find(ctx, accountID)
	if err != nil {
		return AccountView{}, err
	}

	return s.describeOne(ctx, found)
}

// Create makes an account. Nothing works on it until it is given roles and a key.
func (s *Service) Create(ctx context.Context, input SaveInput) (AccountView, error) {
	input.sanitize()

	err := s.check(input)
	if err != nil {
		return AccountView{}, err
	}

	actor, err := s.manager(ctx)
	if err != nil {
		return AccountView{}, err
	}

	err = s.checkPlaces(ctx, 0, input)
	if err != nil {
		return AccountView{}, err
	}

	account := Account{
		ID: 0, Name: input.Name, Description: input.Description, TenantID: tenantOf(input), OwnerID: input.OwnerID,
		CreatedBy: actor.ID, CreatedAt: s.deps.Clock.Now(),
		UpdatedBy: 0, UpdatedAt: nil, DeactivatedAt: nil, DeactivatedBy: 0,
	}

	err = s.deps.Transact.WithinTransaction(ctx, func(ctx context.Context) error {
		err := s.deps.Store.Create(ctx, &account)
		if err != nil {
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
func (s *Service) Update(ctx context.Context, accountID int, input SaveInput) (AccountView, error) {
	input.sanitize()

	err := s.check(input)
	if err != nil {
		return AccountView{}, err
	}

	actor, err := s.manager(ctx)
	if err != nil {
		return AccountView{}, err
	}

	found, err := s.find(ctx, accountID)
	if err != nil {
		return AccountView{}, err
	}

	err = s.checkPlaces(ctx, accountID, input)
	if err != nil {
		return AccountView{}, err
	}

	before, after := found.Account, found.Account
	after.Name, after.Description = input.Name, input.Description
	after.TenantID, after.OwnerID = tenantOf(input), input.OwnerID
	after.UpdatedBy = actor.ID
	now := s.deps.Clock.Now()
	after.UpdatedAt = &now

	err = s.deps.Transact.WithinTransaction(ctx, func(ctx context.Context) error {
		err := s.deps.Store.Update(ctx, after)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}

		entry := audit.NewEntry(AccountEntity, accountID, actor.ID, "update").
			WithBefore(before).WithAfter(after).WithDiff(audit.Diff(before, after))

		return s.deps.Trail.Write(ctx, entry)
	})
	if err != nil {
		return AccountView{}, apperr.Internal(fmt.Errorf("serviceaccount: update: %w", err))
	}

	return s.Get(ctx, accountID)
}

// Deactivate stops the account: every key of it stops working at once, the roles it was given stay so that it
// can be looked at. Deactivating twice keeps the first.
func (s *Service) Deactivate(ctx context.Context, accountID int) (AccountView, error) {
	actor, err := s.manager(ctx)
	if err != nil {
		return AccountView{}, err
	}

	found, err := s.find(ctx, accountID)
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
		err := s.deps.Store.Update(ctx, after)
		if err != nil {
			return fmt.Errorf("deactivate: %w", err)
		}

		entry := audit.NewEntry(AccountEntity, accountID, actor.ID, "deactivate").
			WithBefore(before).WithAfter(after).WithDiff(audit.Diff(before, after))

		return s.deps.Trail.Write(ctx, entry)
	})
	if err != nil {
		return AccountView{}, apperr.Internal(fmt.Errorf("serviceaccount: deactivate: %w", err))
	}

	return s.Get(ctx, accountID)
}

// Keys lists an account's keys, newest first.
func (s *Service) Keys(ctx context.Context, accountID int) ([]KeyView, error) {
	_, err := s.find(ctx, accountID)
	if err != nil {
		return nil, err
	}

	keys, err := s.deps.Store.Keys(ctx, accountID)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("serviceaccount: keys of %d: %w", accountID, err))
	}

	return s.describeKeys(ctx, keys)
}

// CreateKey makes a key and returns its secret, once. Only the hash is stored.
func (s *Service) CreateKey(ctx context.Context, accountID int, input KeyInput) (CreatedKey, error) {
	input.Name = strings.TrimSpace(input.Name)

	actor, found, err := s.keyTarget(ctx, accountID, input.Name)
	if err != nil {
		return CreatedKey{}, err
	}

	now := s.deps.Clock.Now()

	expires, err := keyExpiry(input.ExpiresAt, now)
	if err != nil {
		return CreatedKey{}, err
	}

	if found.ActiveKeys >= MaxActiveKeys {
		return CreatedKey{}, apperr.BadRequest(fmt.Sprintf("at most %d keys work at once; revoke one first", MaxActiveKeys))
	}

	secret, err := s.newSecret()
	if err != nil {
		return CreatedKey{}, err
	}

	key := Key{
		ID: 0, ServiceAccountID: accountID, Name: input.Name, Prefix: PrefixOf(secret), Hash: HashSecret(secret),
		CreatedBy: actor.ID, CreatedAt: now, LastUsedAt: nil, ExpiresAt: expires, RevokedAt: nil, RevokedBy: 0,
	}

	err = s.storeKey(ctx, actor, &key)
	if err != nil {
		return CreatedKey{}, err
	}

	views, err := s.describeKeys(ctx, []Key{key})
	if err != nil {
		return CreatedKey{}, err
	}

	return CreatedKey{Key: views[0], Secret: secret}, nil
}

// RevokeKey stops one key for good. Revoking a revoked key is not an error.
func (s *Service) RevokeKey(ctx context.Context, accountID, keyID int) error {
	actor, err := s.revoker(ctx)
	if err != nil {
		return err
	}

	key, err := s.keyToRevoke(ctx, accountID, keyID)
	if err != nil {
		return err
	}

	if key.RevokedAt != nil {
		return nil
	}

	revokedBy := 0
	if actor.Kind == authz.KindUser {
		revokedBy = actor.ID
	}

	err = s.deps.Transact.WithinTransaction(ctx, func(ctx context.Context) error {
		now := s.deps.Clock.Now()

		err := s.deps.Store.RevokeKey(ctx, keyID, now, revokedBy)
		if err != nil {
			return fmt.Errorf("revoke: %w", err)
		}

		after := key
		after.RevokedAt, after.RevokedBy = &now, revokedBy

		entry := audit.NewEntry(KeyEntity, keyID, revokedBy, "revoke").WithBefore(key).WithAfter(after)

		return s.deps.Trail.Write(ctx, entry)
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

// use hands fn the application's authorizer, or fails when it has none.
func (s *Service) use(fn func(auth authz.Authorizer) error) error {
	if s.deps.Authorizer != nil {
		if auth := s.deps.Authorizer(); auth != nil {
			return fn(auth)
		}
	}

	return apperr.Internal(errNoAuthorizer)
}

// require refuses the actor who does not hold the permission. The refusal keeps its status and reason through
// the wrapping: the error handler finds it with errors.As.
func (s *Service) require(ctx context.Context, permission string) error {
	return s.use(func(auth authz.Authorizer) error {
		err := auth.Require(ctx, permission)
		if err != nil {
			return fmt.Errorf("serviceaccount: require %s: %w", permission, err)
		}

		return nil
	})
}

// access is where the actor holds the permission.
func (s *Service) access(ctx context.Context, permission string) (authz.AccessSet, error) {
	var set authz.AccessSet

	err := s.use(func(auth authz.Authorizer) error {
		var err error

		set, err = auth.Access(ctx, permission)
		if err != nil {
			return fmt.Errorf("serviceaccount: access %s: %w", permission, err)
		}

		return nil
	})

	return set, err
}

// visibility is which accounts the actor may see: every one when they hold the view permission at a place
// above the tenants, otherwise those of the tenants they hold it at.
func (s *Service) visibility(ctx context.Context) (Visibility, error) {
	err := s.require(ctx, View)
	if err != nil {
		return Visibility{All: false, Tenants: nil}, err
	}

	if s.cfg.Tenants == nil {
		return Visibility{All: true, Tenants: nil}, nil
	}

	set, err := s.access(ctx, View)
	if err != nil {
		return Visibility{All: false, Tenants: nil}, err
	}

	var out Visibility

	seen := map[int]bool{}

	for _, clause := range set.Clauses {
		place, found := clause.Chain.At(s.cfg.Tenants.Level)
		if !found {
			return Visibility{All: true, Tenants: nil}, nil
		}

		if !seen[place.ID] {
			seen[place.ID] = true
			out.Tenants = append(out.Tenants, place.ID)
		}
	}

	return out, nil
}

// find reads an account the actor may see: an account of another tenant is a not-found, not a refusal that
// admits it exists.
func (s *Service) find(ctx context.Context, accountID int) (Summary, error) {
	visible, err := s.visibility(ctx)
	if err != nil {
		return Summary{}, err
	}

	found, err := s.deps.Store.Find(ctx, accountID, s.deps.Clock.Now())
	if errors.Is(err, ErrNotFound) {
		return Summary{}, apperr.NotFound("service account not found")
	}

	if err != nil {
		return Summary{}, apperr.Internal(fmt.Errorf("serviceaccount: read %d: %w", accountID, err))
	}

	if !visible.Sees(found.Account) {
		return Summary{}, apperr.NotFound("service account not found")
	}

	return found, nil
}

// manager is the person who may change accounts: they hold the permission and are a person. A service
// account never mints credentials.
func (s *Service) manager(ctx context.Context) (authz.Principal, error) {
	err := s.require(ctx, Manage)
	if err != nil {
		return authz.Principal{}, err
	}

	actor, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return authz.Principal{}, apperr.Unauthorized("not authenticated")
	}

	if actor.Kind != authz.KindUser {
		return authz.Principal{}, apperr.ForbiddenFrom(errPersonOnly).
			WithReason(authz.ReasonForbidden, map[string]any{"why": "person_only"})
	}

	return actor, nil
}

// check validates the input the way the route's tags do, for code that calls the Service.
func (s *Service) check(input SaveInput) error {
	failures := map[string]string{}

	switch {
	case input.Name == "":
		failures["name"] = "required"
	case utf8.RuneCountInString(input.Name) > NameMax:
		failures["name"] = "max"
	}

	if utf8.RuneCountInString(input.Description) > DescriptionMax {
		failures["description"] = "max"
	}

	if input.OwnerID < 1 {
		failures["owner_id"] = "required"
	}

	if len(failures) > 0 {
		return apperr.BadRequest(fmt.Sprintf("invalid service account: %v", failures))
	}

	return nil
}

// checkPlaces refuses a tenant or an owner that does not exist, and a tenant the application refuses.
func (s *Service) checkPlaces(ctx context.Context, accountID int, input SaveInput) error {
	if input.TenantID != nil {
		err := s.checkTenant(ctx, accountID, *input.TenantID)
		if err != nil {
			return err
		}
	}

	people, err := s.deps.People.Names(ctx, []int{input.OwnerID})
	if err != nil {
		return apperr.Internal(fmt.Errorf("serviceaccount: owner: %w", err))
	}

	if _, ok := people[input.OwnerID]; !ok {
		return apperr.BadRequest("owner_id: no such person")
	}

	return nil
}

func (s *Service) checkTenant(ctx context.Context, accountID, tenantID int) error {
	tenants := s.cfg.Tenants
	if tenants == nil {
		return apperr.BadRequest("tenant_id: this application has no tenants")
	}

	names, err := tenants.Names(ctx, []int{tenantID})
	if err != nil {
		return apperr.Internal(fmt.Errorf("serviceaccount: tenant: %w", err))
	}

	if _, ok := names[tenantID]; !ok {
		return apperr.BadRequest("tenant_id: no such tenant")
	}

	if tenants.Confine != nil {
		return tenants.Confine(ctx, accountID, tenantID)
	}

	return nil
}

// keyTarget is the person who may mint a key, and the active account it is for.
func (s *Service) keyTarget(ctx context.Context, accountID int, name string) (authz.Principal, Summary, error) {
	if name == "" || utf8.RuneCountInString(name) > NameMax {
		return authz.Principal{}, Summary{}, apperr.BadRequest("name: required, at most 100 characters")
	}

	actor, err := s.manager(ctx)
	if err != nil {
		return authz.Principal{}, Summary{}, err
	}

	found, err := s.find(ctx, accountID)
	if err != nil {
		return authz.Principal{}, Summary{}, err
	}

	if !found.Active() {
		return authz.Principal{}, Summary{}, apperr.BadRequest("the service account is deactivated")
	}

	return actor, found, nil
}

// storeKey stores the key and records it in the trail, in one transaction.
func (s *Service) storeKey(ctx context.Context, actor authz.Principal, key *Key) error {
	err := s.deps.Transact.WithinTransaction(ctx, func(ctx context.Context) error {
		err := s.deps.Store.CreateKey(ctx, key)
		if err != nil {
			return fmt.Errorf("create key: %w", err)
		}

		// the trail never holds the hash, let alone the secret
		trailed := *key
		trailed.Hash = ""

		return s.deps.Trail.Write(ctx, audit.NewEntry(KeyEntity, key.ID, actor.ID, "create").WithAfter(trailed))
	})
	if err != nil {
		return apperr.Internal(fmt.Errorf("serviceaccount: create key: %w", err))
	}

	return nil
}

// revoker is the principal who may revoke keys: anybody holding the permission, a service account included.
func (s *Service) revoker(ctx context.Context) (authz.Principal, error) {
	err := s.require(ctx, Revoke)
	if err != nil {
		return authz.Principal{}, err
	}

	actor, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return authz.Principal{}, apperr.Unauthorized("not authenticated")
	}

	return actor, nil
}

// keyToRevoke reads the key of an account the actor may see.
func (s *Service) keyToRevoke(ctx context.Context, accountID, keyID int) (Key, error) {
	_, err := s.find(ctx, accountID)
	if err != nil {
		return Key{}, err
	}

	key, err := s.deps.Store.FindKey(ctx, accountID, keyID)
	if errors.Is(err, ErrKeyNotFound) {
		return Key{}, apperr.NotFound("key not found")
	}

	if err != nil {
		return Key{}, apperr.Internal(fmt.Errorf("serviceaccount: read key %d: %w", keyID, err))
	}

	return key, nil
}

// newSecret is a new API key secret: 256 random bits behind the recognisable prefix.
func (s *Service) newSecret() (string, error) {
	raw := make([]byte, secretBytes)

	_, err := rand.Read(raw)
	if err != nil {
		return "", apperr.Internal(fmt.Errorf("serviceaccount: secret: %w", err))
	}

	return s.prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// describe names the tenants and the people of the accounts, in one lookup each.
func (s *Service) describe(ctx context.Context, found []Summary) ([]AccountView, error) {
	tenantIDs := make([]int, 0, len(found))
	personIDs := make([]int, 0, personsPerAccount*len(found))

	for _, item := range found {
		if item.TenantID > 0 {
			tenantIDs = append(tenantIDs, item.TenantID)
		}

		personIDs = append(personIDs, item.OwnerID, item.CreatedBy)
	}

	var tenants map[int]string

	if len(tenantIDs) > 0 && s.cfg.Tenants != nil {
		var err error

		tenants, err = s.cfg.Tenants.Names(ctx, tenantIDs)
		if err != nil {
			return nil, apperr.Internal(fmt.Errorf("serviceaccount: tenant names: %w", err))
		}
	}

	people, err := s.deps.People.Names(ctx, personIDs)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("serviceaccount: people names: %w", err))
	}

	out := make([]AccountView, 0, len(found))

	for _, item := range found {
		view := AccountView{
			ID: item.ID, Name: item.Name, Description: item.Description, Tenant: nil,
			Owner:     Person{ID: item.OwnerID, Name: people[item.OwnerID]},
			CreatedBy: Person{ID: item.CreatedBy, Name: people[item.CreatedBy]},
			CreatedAt: item.CreatedAt, ActiveKeys: item.ActiveKeys, LastUsedAt: item.LastUsedAt,
			DeactivatedAt: item.DeactivatedAt,
		}

		if item.TenantID > 0 {
			view.Tenant = &TenantRef{ID: item.TenantID, Name: tenants[item.TenantID]}
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

func tenantOf(input SaveInput) int {
	if input.TenantID == nil {
		return 0
	}

	return *input.TenantID
}

// keyExpiry is the expiry a key is given: nothing for a key that never expires, otherwise a time in the future
// and within maxKeyLife.
func keyExpiry(requested *time.Time, now time.Time) (*time.Time, error) {
	var never *time.Time

	if requested == nil || requested.IsZero() {
		return never, nil
	}

	expiry := requested.UTC()
	if !expiry.After(now) || expiry.After(now.Add(maxKeyLife)) {
		return never, apperr.BadRequest("expires_at must be in the future, within five years")
	}

	return &expiry, nil
}
