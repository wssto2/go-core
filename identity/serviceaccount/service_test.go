//nolint:exhaustruct,exhaustruct_v5,funlen,gochecknoglobals,goconst,lll,musttag,paralleltest,unparam,varnamelen,wrapcheck // tests in the style of the package's others
package serviceaccount_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/identity/serviceaccount"
	"github.com/wssto2/go-core/identity/serviceaccount/gormstore"
)

var now = time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

type clock struct{}

func (clock) Now() time.Time { return now }

// people are the persons 1 to 9.
type people struct{}

func (people) Names(_ context.Context, ids []int) (map[int]string, error) {
	out := map[int]string{}

	for _, id := range ids {
		if id >= 1 && id <= 9 {
			out[id] = "Person"
		}
	}

	return out, nil
}

// rights is the authorizer of a test: the permissions held, and the clauses the view permission is held through.
type rights struct {
	held    map[string]bool
	clauses []authz.Clause
}

func (r rights) Require(_ context.Context, p string) error {
	if r.held[p] {
		return nil
	}

	return authz.Deny(p)
}

func (r rights) RequireOn(ctx context.Context, p string, _ authz.Resource) error {
	return r.Require(ctx, p)
}

func (r rights) Access(context.Context, string) (authz.AccessSet, error) {
	return authz.AccessSet{Clauses: r.clauses}, nil
}

type trail struct{ entries []audit.Entry }

func (t *trail) Write(_ context.Context, e audit.Entry) error {
	t.entries = append(t.entries, e)

	return nil
}

type fixture struct {
	svc    *serviceaccount.Service
	trail  *trail
	rights *rights
}

func setup(t *testing.T, cfg serviceaccount.Config) *fixture {
	t.Helper()

	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	db := reg.MustGet("local")
	require.NoError(t, gormstore.Migrate(db))

	f := &fixture{trail: &trail{}, rights: &rights{held: map[string]bool{
		serviceaccount.View: true, serviceaccount.Manage: true, serviceaccount.Revoke: true,
	}}}
	f.svc = serviceaccount.New(serviceaccount.Deps{
		Store: gormstore.New(db), Transact: database.NewTransactor(db), Trail: f.trail, Clock: clock{}, People: people{},
		Authorizer: func() authz.Authorizer { return f.rights },
	}, cfg)

	return f
}

func asUser(id int) context.Context {
	return authz.WithPrincipal(context.Background(), authz.User(id, 0))
}

func save(name string, tenant *int, owner int) serviceaccount.SaveInput {
	return serviceaccount.SaveInput{Name: name, TenantID: tenant, OwnerID: owner}
}

func TestCreateChecksWhoAnswersForTheAccount(t *testing.T) {
	f := setup(t, serviceaccount.Config{})
	ctx := asUser(1)

	made, err := f.svc.Create(ctx, serviceaccount.SaveInput{Name: "  feed  ", Description: " text ", OwnerID: 2})
	require.NoError(t, err)
	assert.Equal(t, "feed", made.Name, "the name is trimmed")
	assert.Equal(t, "text", made.Description)
	assert.Equal(t, serviceaccount.Person{ID: 2, Name: "Person"}, made.Owner)
	assert.Equal(t, serviceaccount.Person{ID: 1, Name: "Person"}, made.CreatedBy)
	assert.Nil(t, made.Tenant)

	require.Len(t, f.trail.entries, 1)
	assert.Equal(t, serviceaccount.AccountEntity, f.trail.entries[0].EntityType)
	assert.Equal(t, 1, f.trail.entries[0].ActorID)
	assert.Equal(t, "create", f.trail.entries[0].Action)

	for name, in := range map[string]serviceaccount.SaveInput{
		"no name":                        save("", nil, 2),
		"a long name":                    save(string(make([]byte, 101)), nil, 2),
		"no owner":                       save("a", nil, 0),
		"an owner who isn't":             save("a", nil, 99),
		"a tenant, where there are none": save("a", new(5), 2),
	} {
		_, err := f.svc.Create(ctx, in)
		require.Error(t, err, name)
		assert.True(t, apperr.HasCode(err, apperr.CodeBadRequest), name)
	}
}

func TestOnlyAPersonWithThePermissionManagesAccounts(t *testing.T) {
	f := setup(t, serviceaccount.Config{})
	robot := authz.WithPrincipal(context.Background(), authz.ServiceAccount(7))

	_, err := f.svc.Create(robot, save("a", nil, 2))
	require.Error(t, err)
	assert.True(t, apperr.HasCode(err, apperr.CodePermissionDenied))
	assert.True(t, apperr.HasReason(err, authz.ReasonForbidden), "a service account never mints credentials")

	f.rights.held[serviceaccount.Manage] = false
	_, err = f.svc.Create(asUser(1), save("a", nil, 2))
	assert.True(t, apperr.HasReason(err, authz.ReasonForbidden))

	_, err = f.svc.Create(context.Background(), save("a", nil, 2))
	require.Error(t, err, "nobody signed in")
}

// Tenants: an administrator of a tenant sees the accounts of the tenant, and nobody else's; the root sees all.
func TestAdministratorsOfATenantSeeTheirTenantsAccounts(t *testing.T) {
	asked := map[int]bool{}

	f := setup(t, serviceaccount.Config{Tenants: &serviceaccount.Tenants{
		Level: "dealer",
		Names: func(_ context.Context, ids []int) (map[int]string, error) {
			out := map[int]string{}

			for _, id := range ids {
				if id == 5 || id == 6 {
					out[id] = "Dealer"
				}
			}

			return out, nil
		},
		Confine: func(_ context.Context, id, tenant int) error {
			asked[tenant] = true

			if tenant == 6 && id != 0 {
				return apperr.BadRequest("holds a role elsewhere")
			}

			return nil
		},
	}})

	org := authz.Scope{Level: "organization"}
	dealer := func(id int) authz.Clause {
		return authz.Clause{Scope: authz.Scope{Level: "dealer", ID: id}, Chain: authz.Chain{{Level: "dealer", ID: id}, org}}
	}

	f.rights.clauses = []authz.Clause{{Scope: org, Chain: authz.Chain{org}}}
	root := asUser(1)

	a, err := f.svc.Create(root, save("of five", new(5), 2))
	require.NoError(t, err)
	assert.Equal(t, &serviceaccount.TenantRef{ID: 5, Name: "Dealer"}, a.Tenant)

	b, err := f.svc.Create(root, save("of six", new(6), 2))
	require.NoError(t, err)

	_, err = f.svc.Create(root, save("nobody's", new(7), 2))
	require.Error(t, err, "no such tenant")

	organization, err := f.svc.Create(root, save("the organization's", nil, 2))
	require.NoError(t, err)

	all, err := f.svc.List(root)
	require.NoError(t, err)
	assert.Len(t, all, 3, "the root sees every account")

	f.rights.clauses = []authz.Clause{dealer(5)}

	mine, err := f.svc.List(root)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, a.ID, mine[0].ID)

	_, err = f.svc.Get(root, b.ID)
	assert.True(t, apperr.HasCode(err, apperr.CodeNotFound), "another tenant's account is not found, not forbidden")

	_, err = f.svc.Get(root, organization.ID)
	assert.True(t, apperr.HasCode(err, apperr.CodeNotFound), "nor is the organization's")

	reach, err := f.svc.Reach(root, a.ID)
	require.NoError(t, err)
	assert.True(t, reach)

	reach, err = f.svc.Reach(root, b.ID)
	require.NoError(t, err)
	assert.False(t, reach)

	// Confine is asked before an account is moved into a tenant
	f.rights.clauses = []authz.Clause{{Scope: org, Chain: authz.Chain{org}}}
	_, err = f.svc.Update(root, a.ID, save("of five", new(6), 2))
	require.Error(t, err)
	assert.True(t, asked[6])

	moved, err := f.svc.Update(root, a.ID, save("renamed", nil, 3))
	require.NoError(t, err)
	assert.Nil(t, moved.Tenant, "back to the organization's")
	assert.Equal(t, "renamed", moved.Name)
	assert.Equal(t, 3, moved.Owner.ID)
}

func TestKeysAreMadeOnceListedAndRevoked(t *testing.T) {
	f := setup(t, serviceaccount.Config{KeyPrefix: "acme_"})
	ctx := asUser(1)

	a, err := f.svc.Create(ctx, save("feed", nil, 2))
	require.NoError(t, err)

	created, err := f.svc.CreateKey(ctx, a.ID, serviceaccount.KeyInput{Name: " ci "})
	require.NoError(t, err)
	assert.Contains(t, created.Secret, "acme_")
	assert.Len(t, created.Secret, len("acme_")+43, "256 random bits")
	assert.Equal(t, created.Secret[:12], created.Key.Prefix)
	assert.Equal(t, "ci", created.Key.Name)
	assert.NotContains(t, created.Key.Prefix, created.Secret[12:])

	// the trail holds neither the hash nor the secret
	last := f.trail.entries[len(f.trail.entries)-1]
	assert.Equal(t, serviceaccount.KeyEntity, last.EntityType)
	assert.NotContains(t, asText(last), created.Secret)
	assert.NotContains(t, asText(last), serviceaccount.HashSecret(created.Secret))

	keys, err := f.svc.Keys(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)

	for name, expires := range map[string]time.Time{"in the past": now.Add(-time.Hour), "beyond five years": now.Add(6 * 365 * 24 * time.Hour)} {
		_, err := f.svc.CreateKey(ctx, a.ID, serviceaccount.KeyInput{Name: "late", ExpiresAt: &expires})
		require.Error(t, err, name)
	}

	soon := now.Add(24 * time.Hour)
	_, err = f.svc.CreateKey(ctx, a.ID, serviceaccount.KeyInput{Name: "timed", ExpiresAt: &soon})
	require.NoError(t, err)

	// a key works until it is revoked; revoking twice is not an error
	caller, err := f.svc.Authenticate(ctx, created.Secret)
	require.NoError(t, err)
	assert.Equal(t, serviceaccount.Caller{AccountID: a.ID, KeyID: created.Key.ID, Name: "feed"}, caller)
	assert.Zero(t, caller.GetID())
	assert.Equal(t, "service:"+itoa(a.ID), caller.RateLimitKey())
	assert.False(t, caller.HasTenant())

	require.NoError(t, f.svc.RevokeKey(ctx, a.ID, created.Key.ID))
	require.NoError(t, f.svc.RevokeKey(ctx, a.ID, created.Key.ID))
	assert.True(t, apperr.HasCode(f.svc.RevokeKey(ctx, a.ID, 99), apperr.CodeNotFound))

	_, err = f.svc.Authenticate(ctx, created.Secret)
	assert.True(t, apperr.HasCode(err, apperr.CodeUnauthenticated))

	_, err = f.svc.Authenticate(ctx, "")
	assert.True(t, apperr.HasCode(err, apperr.CodeUnauthenticated))
}

func TestAtMostTenKeysWorkAtOnce(t *testing.T) {
	f := setup(t, serviceaccount.Config{})
	ctx := asUser(1)

	a, err := f.svc.Create(ctx, save("feed", nil, 2))
	require.NoError(t, err)

	for range serviceaccount.MaxActiveKeys {
		_, err := f.svc.CreateKey(ctx, a.ID, serviceaccount.KeyInput{Name: "k"})
		require.NoError(t, err)
	}

	_, err = f.svc.CreateKey(ctx, a.ID, serviceaccount.KeyInput{Name: "one more"})
	require.Error(t, err)
	assert.True(t, apperr.HasCode(err, apperr.CodeBadRequest))
}

func TestADeactivatedAccountsKeysStop(t *testing.T) {
	f := setup(t, serviceaccount.Config{})
	ctx := asUser(1)

	a, err := f.svc.Create(ctx, save("feed", nil, 2))
	require.NoError(t, err)

	created, err := f.svc.CreateKey(ctx, a.ID, serviceaccount.KeyInput{Name: "ci"})
	require.NoError(t, err)

	off, err := f.svc.Deactivate(ctx, a.ID)
	require.NoError(t, err)
	require.NotNil(t, off.DeactivatedAt)

	again, err := f.svc.Deactivate(ctx, a.ID)
	require.NoError(t, err)
	assert.True(t, again.DeactivatedAt.Equal(*off.DeactivatedAt), "twice keeps the first")

	_, err = f.svc.Authenticate(ctx, created.Secret)
	assert.True(t, apperr.HasCode(err, apperr.CodeUnauthenticated))

	_, err = f.svc.CreateKey(ctx, a.ID, serviceaccount.KeyInput{Name: "more"})
	require.Error(t, err, "a deactivated account gets no key")

	names, err := f.svc.Names(ctx, []int{a.ID, 99})
	require.NoError(t, err)
	assert.Equal(t, map[int]string{a.ID: "feed"}, names, "it is still a subject, to be looked at")
}

func itoa(n int) string { return strconv.Itoa(n) }

func asText(e audit.Entry) string {
	raw, err := json.Marshal(e)
	if err != nil {
		return err.Error()
	}

	return string(raw)
}
