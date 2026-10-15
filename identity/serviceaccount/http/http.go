// Package http is the HTTP side of service accounts: the typed routes under /v1/iam/service-accounts, their
// inputs and response types, and the handlers over the Service. The routes are declared as values so the
// TypeScript contract is generated from them without starting anything, and bound to the service with Routes.
//
// Reading is iam.serviceaccount:view (an administrator of a tenant sees the tenant's accounts), creating,
// editing, deactivating and creating keys is :manage, revoking a key is :revoke. The access of an account
// (what roles it holds, and where) is the access module's: access.Install mounts those routes under the same
// path (access/accesshttp) when service accounts are installed.
package http

import (
	"context"
	nethttp "net/http"
	"time"

	"github.com/wssto2/go-core/access/accesshttp"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/identity/serviceaccount"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/validation"
)

const (
	base   = "/v1/iam/service-accounts"
	record = base + "/:id"
)

// AccountPath names a service account.
type AccountPath struct {
	ID int `path:"id"`
}

// UpdateRequest is the account and the input to save.
type UpdateRequest struct {
	AccountPath
	serviceaccount.SaveInput
}

// KeyRequest is the account and the key to create.
type KeyRequest struct {
	AccountPath
	serviceaccount.KeyInput
}

// RevokeRequest names a key of an account.
type RevokeRequest struct {
	AccountPath

	KeyID int `path:"keyID"`
}

// The service account routes. A path id that is not a number answers 400, for the whole group.
var (
	Index = route.Get[route.None, ServiceAccountList](base).Name("serviceaccount.index").Requires(serviceaccount.View)
	Show  = route.Get[AccountPath, ServiceAccount](record).Name("serviceaccount.show").Requires(serviceaccount.View)
	// Create makes an account, answering 201.
	Create     = route.Post[serviceaccount.SaveInput, ServiceAccount](base).Name("serviceaccount.create").Requires(serviceaccount.Manage).Status(nethttp.StatusCreated)
	Update     = route.Put[UpdateRequest, ServiceAccount](record).Name("serviceaccount.update").Requires(serviceaccount.Manage)
	Deactivate = route.Post[AccountPath, ServiceAccount](record + "/deactivate").Name("serviceaccount.deactivate").Requires(serviceaccount.Manage)
	Keys       = route.Get[AccountPath, ApiKeyList](record + "/keys").Name("serviceaccount.keys").Requires(serviceaccount.View)
	RevokeKey  = route.Post[RevokeRequest, route.Empty](record + "/keys/:keyID/revoke").Name("serviceaccount.revokekey").Requires(serviceaccount.Revoke)

	// CreateKey answers with the new key's secret, shown once, and Cache-Control: no-store.
	CreateKey = route.Post[KeyRequest, CreatedApiKey](record + "/keys").Name("serviceaccount.createkey").Requires(serviceaccount.Manage).Status(nethttp.StatusCreated)

	// Contract is what the module offers the outside: contract.Generate(dir, serviceaccount.Routes) writes its TypeScript.
	// It lists the access routes of an account too, which access.Install serves.
	Contract = route.Group("serviceaccount", Index, Show, Create, Update, Deactivate, Keys, CreateKey, RevokeKey,
		accesshttp.ServiceAccountBindableRoles, accesshttp.ServiceAccountAccess, accesshttp.ServiceAccountScopes,
		accesshttp.ServiceAccountBind, accesshttp.ServiceAccountUnbind).MalformedPath(nethttp.StatusBadRequest)
)

// PersonRef is a person by name.
type PersonRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// AccountTenant is the tenant an account is confined to.
type AccountTenant struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ServiceAccount is a system that calls the API with keys. A missing tenant is the organization's account; a
// deactivated account's keys no longer work.
type ServiceAccount struct {
	ID            int            `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Tenant        *AccountTenant `json:"tenant"`
	Owner         PersonRef      `json:"owner"`
	CreatedBy     PersonRef      `json:"created_by"`
	CreatedAt     time.Time      `json:"created_at"`
	ActiveKeys    int            `json:"active_keys"`
	LastUsedAt    *time.Time     `json:"last_used_at"`
	DeactivatedAt *time.Time     `json:"deactivated_at"`
}

// ServiceAccountList is the accounts of GET /service-accounts.
type ServiceAccountList struct {
	ServiceAccounts []ServiceAccount `json:"service_accounts"`
}

// ApiKey is an API key without its secret.
type ApiKey struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedBy  PersonRef  `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// ApiKeyList is the keys of GET /service-accounts/:id/keys.
type ApiKeyList struct {
	Keys []ApiKey `json:"keys"`
}

// CreatedApiKey is a new key with its secret: the only time the secret is ever returned.
type CreatedApiKey struct {
	Key    ApiKey `json:"key"`
	Secret string `json:"secret"`
}

func accountOf(a serviceaccount.AccountView) ServiceAccount {
	out := ServiceAccount{
		ID: a.ID, Name: a.Name, Description: a.Description, Owner: PersonRef(a.Owner), CreatedBy: PersonRef(a.CreatedBy),
		CreatedAt: a.CreatedAt, ActiveKeys: a.ActiveKeys, LastUsedAt: a.LastUsedAt, DeactivatedAt: a.DeactivatedAt,
	}

	if a.Tenant != nil {
		out.Tenant = &AccountTenant{ID: a.Tenant.ID, Name: a.Tenant.Name}
	}

	return out
}

func keyOf(k serviceaccount.KeyView) ApiKey {
	return ApiKey{
		ID: k.ID, Name: k.Name, Prefix: k.Prefix, CreatedBy: PersonRef(k.CreatedBy), CreatedAt: k.CreatedAt,
		LastUsedAt: k.LastUsedAt, ExpiresAt: k.ExpiresAt, RevokedAt: k.RevokedAt,
	}
}

// Accounts is what the routes ask of the module: *serviceaccount.Service.
type Accounts interface {
	List(ctx context.Context) ([]serviceaccount.AccountView, error)
	Get(ctx context.Context, id int) (serviceaccount.AccountView, error)
	Create(ctx context.Context, in serviceaccount.SaveInput) (serviceaccount.AccountView, error)
	Update(ctx context.Context, id int, in serviceaccount.SaveInput) (serviceaccount.AccountView, error)
	Deactivate(ctx context.Context, id int) (serviceaccount.AccountView, error)
	Keys(ctx context.Context, id int) ([]serviceaccount.KeyView, error)
	CreateKey(ctx context.Context, id int, in serviceaccount.KeyInput) (serviceaccount.CreatedKey, error)
	RevokeKey(ctx context.Context, id, keyID int) error
}

// Routes binds the handlers of the routes to the service; hand the result to app.Routes.
func Routes(accounts Accounts) []route.Handled {
	h := handler{accounts: accounts}

	return []route.Handled{
		Index.To(h.index), Show.To(h.show), Create.To(h.create), Update.To(h.update), Deactivate.To(h.deactivate),
		Keys.To(h.keys), CreateKey.To(h.createKey), RevokeKey.To(h.revokeKey),
	}
}

type handler struct{ accounts Accounts }

// positive refuses a path id that is zero or negative: the account of such an id cannot exist.
func positive(name string, id int) error {
	if id <= 0 {
		return apperr.BadRequest("invalid " + name + ": must be a positive integer")
	}

	return nil
}

func (h handler) index(ctx context.Context, _ route.None) (ServiceAccountList, error) {
	accounts, err := h.accounts.List(ctx)
	if err != nil {
		return ServiceAccountList{}, err
	}

	out := ServiceAccountList{ServiceAccounts: make([]ServiceAccount, 0, len(accounts))}
	for _, a := range accounts {
		out.ServiceAccounts = append(out.ServiceAccounts, accountOf(a))
	}

	return out, nil
}

func (h handler) show(ctx context.Context, in AccountPath) (ServiceAccount, error) {
	if err := positive("id", in.ID); err != nil {
		return ServiceAccount{}, err
	}

	a, err := h.accounts.Get(ctx, in.ID)

	return accountOf(a), err
}

func (h handler) create(ctx context.Context, in serviceaccount.SaveInput) (ServiceAccount, error) {
	a, err := h.accounts.Create(ctx, in)

	return accountOf(a), err
}

func (h handler) update(ctx context.Context, in UpdateRequest) (ServiceAccount, error) {
	if err := positive("id", in.ID); err != nil {
		return ServiceAccount{}, err
	}

	a, err := h.accounts.Update(ctx, in.ID, in.SaveInput)

	return accountOf(a), err
}

func (h handler) deactivate(ctx context.Context, in AccountPath) (ServiceAccount, error) {
	if err := positive("id", in.ID); err != nil {
		return ServiceAccount{}, err
	}

	a, err := h.accounts.Deactivate(ctx, in.ID)

	return accountOf(a), err
}

func (h handler) keys(ctx context.Context, in AccountPath) (ApiKeyList, error) {
	if err := positive("id", in.ID); err != nil {
		return ApiKeyList{}, err
	}

	keys, err := h.accounts.Keys(ctx, in.ID)
	if err != nil {
		return ApiKeyList{}, err
	}

	out := ApiKeyList{Keys: make([]ApiKey, 0, len(keys))}
	for _, k := range keys {
		out.Keys = append(out.Keys, keyOf(k))
	}

	return out, nil
}

func (h handler) revokeKey(ctx context.Context, in RevokeRequest) (route.Empty, error) {
	if err := positive("id", in.ID); err != nil {
		return route.Empty{}, err
	}

	if in.KeyID <= 0 { // the 422 a path number of the route package gives
		return route.Empty{}, validation.NewValidationError("validation failed",
			map[string][]validation.Failure{"keyID": {validation.Fail(validation.CodeMustBePositiveNumber)}}, map[string][]string{"keyID": {"path"}})
	}

	return route.Empty{}, h.accounts.RevokeKey(ctx, in.ID, in.KeyID)
}

// createKey answers the secret with Cache-Control: no-store (it is shown once: never cache it), and only
// when the key was made: an error answer is no secret.
func (h handler) createKey(ctx context.Context, in KeyRequest) (CreatedApiKey, error) {
	if err := positive("id", in.ID); err != nil {
		return CreatedApiKey{}, err
	}

	created, err := h.accounts.CreateKey(ctx, in.ID, in.KeyInput)
	if err != nil {
		return CreatedApiKey{}, err
	}

	if err := route.ExchangeOf(ctx).SetHeader("Cache-Control", "no-store"); err != nil {
		return CreatedApiKey{}, err
	}

	return CreatedApiKey{Key: keyOf(created.Key), Secret: created.Secret}, nil
}
