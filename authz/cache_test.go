package authz_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

// countingStore counts reads of bindings.
type countingStore struct {
	*authztest.MemoryStore

	reads atomic.Int64
}

func (c *countingStore) BindingsFor(ctx context.Context, s authz.Subject) ([]authz.Binding, error) {
	c.reads.Add(1)
	return c.MemoryStore.BindingsFor(ctx, s)
}

func TestEngineCachesEffectiveAccess(t *testing.T) {
	p := authztestParts(t)
	store := &countingStore{MemoryStore: p.store}
	p.places.AddDealer(1)
	e, err := authz.NewEngine(authz.Config{Catalogue: smallCatalogue(t), Hierarchy: p.h, Roles: smallRoles(), Store: store, Resolver: p.places, Features: p.features})
	require.NoError(t, err)
	_, err = store.Bind(context.Background(), authz.Subject{}, authz.Binding{Subject: user(1), Role: authz.RoleRef{Key: "seller"}, Scope: authztest.Dealer(1)})
	require.NoError(t, err)
	ctx := authz.WithPrincipal(context.Background(), authz.User(1, 0))

	for range 5 {
		require.NoError(t, e.Require(ctx, "vehicle.stock:view"))
	}
	assert.EqualValues(t, 1, store.reads.Load(), "one load, four cache hits")

	e.Evict(user(1))
	require.NoError(t, e.Require(ctx, "vehicle.stock:view"))
	assert.EqualValues(t, 2, store.reads.Load())

	e.EvictAll()
	require.NoError(t, e.Require(ctx, "vehicle.stock:view"))
	assert.EqualValues(t, 3, store.reads.Load())
}

func TestConcurrentChecksAndEvictions(t *testing.T) {
	w := newSmallWorld(t)
	const users = 8
	for i := 1; i <= users; i++ {
		w.Bind(user(i), "seller", authztest.Dealer(1))
	}
	role := w.SaveRole(authz.Role{Name: "extra", Grants: authz.Grants(authz.QualifierAll, "crm.offer:view")})
	for i := 1; i <= users; i++ {
		w.BindCustom(user(i), role.ID, authztest.Dealer(1))
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 1; i <= users; i++ {
		wg.Go(func() {
			ctx := w.As(authz.User(i, 0))
			for {
				select {
				case <-stop:
					return
				default:
				}
				assert.NoError(t, w.Engine.Require(ctx, "vehicle.stock:view"))
				_, err := w.Engine.Access(ctx, "crm.lead:view")
				assert.NoError(t, err)
				assert.NoError(t, w.Engine.RequireOn(ctx, "vehicle.stock:view", authz.Resource{Scope: authztest.Location(10)}))
			}
		})
	}
	for range 200 {
		w.Engine.EvictRole(role.ID)
		w.Engine.Evict(user(1))
		w.Engine.EvictAll()
	}
	close(stop)
	wg.Wait()
}

func TestEvictionBeatsAConcurrentLoad(t *testing.T) {
	w := newSmallWorld(t)
	b := w.Bind(user(1), "seller", authztest.Dealer(1))
	ctx := w.As(authz.User(1, 0))
	require.NoError(t, w.Engine.Require(ctx, "vehicle.stock:view"))

	// remove the binding behind the engine's back, then evict as Admin would
	require.NoError(t, w.Store.Unbind(context.Background(), authz.Subject{}, b.ID))
	w.Engine.Evict(user(1))
	assert.ErrorIs(t, w.Engine.Require(ctx, "vehicle.stock:view"), authz.ErrForbidden)
}
