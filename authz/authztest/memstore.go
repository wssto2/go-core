package authztest

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/wssto2/go-core/authz"
)

// MemoryStore is an in-memory authz.Store. It passes the conformance suite in
// authz/storetest, so it behaves like a real store in an application's tests.
type MemoryStore struct {
	mu       sync.Mutex
	roles    map[int]authz.Role
	bindings map[int]authz.Binding
	nextRole int
	nextBind int
	now      func() time.Time
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{roles: map[int]authz.Role{}, bindings: map[int]authz.Binding{}, now: time.Now}
}

var (
	_ authz.Store       = (*MemoryStore)(nil)
	_ authz.HolderStore = (*MemoryStore)(nil)
)

func copyRole(r authz.Role) authz.Role {
	r.Grants = slices.Clone(r.Grants)
	if r.Attrs != nil {
		attrs := make(map[string][]string, len(r.Attrs))
		for k, v := range r.Attrs {
			attrs[k] = slices.Clone(v)
		}
		r.Attrs = attrs
	}
	return r
}

// BindingsFor implements authz.Reader.
func (m *MemoryStore) BindingsFor(_ context.Context, s authz.Subject) ([]authz.Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.filterBindings(func(b authz.Binding) bool { return b.Subject == s }), nil
}

// Role implements authz.Reader.
func (m *MemoryStore) Role(_ context.Context, id int) (authz.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.roles[id]
	if !ok {
		return authz.Role{}, fmt.Errorf("%w: id %d", authz.ErrRoleNotFound, id)
	}
	return copyRole(r), nil
}

// ListRoles implements authz.Store.
func (m *MemoryStore) ListRoles(_ context.Context) ([]authz.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]authz.Role, 0, len(m.roles))
	for _, r := range m.roles {
		out = append(out, copyRole(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// SaveRole implements authz.Store.
func (m *MemoryStore) SaveRole(_ context.Context, _ authz.Subject, role authz.Role) (authz.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if role.ID == 0 {
		m.nextRole++
		role.ID = m.nextRole
	} else if _, ok := m.roles[role.ID]; !ok {
		return authz.Role{}, fmt.Errorf("%w: id %d", authz.ErrRoleNotFound, role.ID)
	}
	role.Key, role.Computed = "", nil
	m.roles[role.ID] = copyRole(role)
	return copyRole(role), nil
}

// DeleteRole implements authz.Store.
func (m *MemoryStore) DeleteRole(_ context.Context, _ authz.Subject, id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.roles[id]; !ok {
		return fmt.Errorf("%w: id %d", authz.ErrRoleNotFound, id)
	}
	if len(m.filterBindings(func(b authz.Binding) bool { return b.Role.ID == id })) > 0 {
		return fmt.Errorf("%w: id %d", authz.ErrRoleInUse, id)
	}
	delete(m.roles, id)
	return nil
}

// Binding implements authz.Store.
func (m *MemoryStore) Binding(_ context.Context, id int) (authz.Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.bindings[id]
	if !ok {
		return authz.Binding{}, fmt.Errorf("%w: id %d", authz.ErrBindingNotFound, id)
	}
	return b, nil
}

// BindingsForRole implements authz.Store.
func (m *MemoryStore) BindingsForRole(_ context.Context, roleID int) ([]authz.Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.filterBindings(func(b authz.Binding) bool { return b.Role.ID == roleID }), nil
}

// HolderCounts implements authz.HolderStore.
func (m *MemoryStore) HolderCounts(_ context.Context) (authz.HolderCounts, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := authz.HolderCounts{ByID: map[int]int{}, ByKey: map[string]int{}}
	seen := map[[2]any]bool{}
	for _, b := range m.bindings {
		who := [2]any{b.Role, b.Subject}
		if seen[who] {
			continue
		}
		seen[who] = true
		if b.Role.ID > 0 {
			out.ByID[b.Role.ID]++
		} else {
			out.ByKey[b.Role.Key]++
		}
	}
	return out, nil
}

// HoldersOf implements authz.HolderStore.
func (m *MemoryStore) HoldersOf(_ context.Context, ref authz.RoleRef) ([]authz.Binding, error) {
	if !ref.Valid() {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.filterBindings(func(b authz.Binding) bool { return b.Role == ref }), nil
}

// Bind implements authz.Store.
func (m *MemoryStore) Bind(_ context.Context, actor authz.Subject, b authz.Binding) (authz.Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.bindings {
		if x.Subject == b.Subject && x.Role == b.Role && x.Scope == b.Scope {
			return authz.Binding{}, authz.ErrDuplicateBinding
		}
	}
	m.nextBind++
	b.ID, b.CreatedBy, b.CreatedAt = m.nextBind, actor.ID, m.now()
	m.bindings[b.ID] = b
	return b, nil
}

// Unbind implements authz.Store.
func (m *MemoryStore) Unbind(_ context.Context, _ authz.Subject, id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.bindings[id]; !ok {
		return fmt.Errorf("%w: id %d", authz.ErrBindingNotFound, id)
	}
	delete(m.bindings, id)
	return nil
}

// filterBindings returns matching bindings ordered by ID. The caller holds mu.
func (m *MemoryStore) filterBindings(keep func(authz.Binding) bool) []authz.Binding {
	var out []authz.Binding
	for _, b := range m.bindings {
		if keep(b) {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
