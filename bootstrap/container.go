package bootstrap

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/wssto2/go-core/database"
)

// providerInfo holds a registered provider function and its declared deps.
type providerInfo struct {
	fn   reflect.Value  // the provider function as a reflect.Value
	out  reflect.Type   // return type T (the key in the providers map)
	deps []reflect.Type // declared input types (dependencies)
}

// Container is a type-safe service container that supports both direct value
// binding (Bind) and provider-function registration (Register).
// Call Build() after all registrations to validate the dependency graph.
type Container struct {
	mu        sync.RWMutex
	direct    map[reflect.Type]any           // values from Bind[S]
	providers map[reflect.Type]*providerInfo // functions from Register()
	instances map[reflect.Type]any           // lazy singleton cache for providers
	strict    bool
}

// NewContainer creates an empty Container.
func NewContainer() *Container {
	return &Container{
		direct:    make(map[reflect.Type]any),
		providers: make(map[reflect.Type]*providerInfo),
		instances: make(map[reflect.Type]any),
	}
}

// EnableStrictMode configures the container to panic on duplicate Bind calls
// and on Resolve calls for unregistered types.
func (c *Container) EnableStrictMode() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.strict = true
}

// Bind registers a concrete value directly. No dependencies are declared.
// In strict mode, panics if the type is already registered.
// Use Rebind for intentional overwrites (e.g., swapping InMemoryBus for NATSBus).
func Bind[S any](c *Container, val S) {
	if c == nil {
		panic("bootstrap: Bind called on nil container")
	}
	typ := reflect.TypeFor[S]()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.direct == nil {
		c.direct = make(map[reflect.Type]any)
	}
	if c.strict {
		if _, exists := c.direct[typ]; exists {
			panic(fmt.Sprintf("bootstrap: duplicate Bind for %v", typ))
		}
	}
	c.direct[typ] = val
}

// OverwriteBind overwrites a previously registered type without panicking in strict mode.
// Use this when intentionally replacing a service binding.
func OverwriteBind[S any](c *Container, val S) {
	if c == nil {
		panic("bootstrap: Rebind called on nil container")
	}
	typ := reflect.TypeFor[S]()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.direct == nil {
		c.direct = make(map[reflect.Type]any)
	}
	c.direct[typ] = val
}

// Register accepts a provider function with signature: func(...deps) (T, error).
// Dependencies are declared implicitly by the function's parameter types.
// Providers are resolved lazily on first Resolve call.
// Call Build() after all Register calls to validate for cycles.
func (c *Container) Register(providerFn any) error {
	if c == nil {
		return fmt.Errorf("bootstrap: Register called on nil container")
	}
	v := reflect.ValueOf(providerFn)
	t := v.Type()
	if t.Kind() != reflect.Func {
		return fmt.Errorf("bootstrap: Register: provider must be a function, got %T", providerFn)
	}
	if t.NumOut() != 2 {
		return fmt.Errorf("bootstrap: Register: provider must return (T, error), got %d outputs", t.NumOut())
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if !t.Out(1).Implements(errorType) {
		return fmt.Errorf("bootstrap: Register: second return value must implement error")
	}
	retType := t.Out(0)
	deps := make([]reflect.Type, t.NumIn())
	for i := 0; i < t.NumIn(); i++ {
		deps[i] = t.In(i)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.strict {
		if _, exists := c.providers[retType]; exists {
			panic(fmt.Sprintf("bootstrap: duplicate Register for %v", retType))
		}
	}
	c.providers[retType] = &providerInfo{fn: v, out: retType, deps: deps}
	return nil
}

// Build validates the provider dependency graph for circular dependencies using
// depth-first search. Only provider-function registrations (Register) can form
// cycles. Direct value bindings (Bind) have no declared dependencies.
// Call Build() once after all Register calls, before any Resolve call.
func (c *Container) Build() error {
	c.mu.RLock()
	graph := make(map[reflect.Type][]reflect.Type, len(c.providers))
	for out, prov := range c.providers {
		for _, d := range prov.deps {
			if _, ok := c.providers[d]; ok {
				graph[out] = append(graph[out], d)
			}
		}
	}
	c.mu.RUnlock()

	state := make(map[reflect.Type]int) // 0=unseen 1=visiting 2=done
	var stack []reflect.Type

	var visit func(reflect.Type) error
	visit = func(n reflect.Type) error {
		if state[n] == 1 {
			// cycle detected — reconstruct the path
			idx := -1
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i] == n {
					idx = i
					break
				}
			}
			var path []string
			if idx >= 0 {
				for i := idx; i < len(stack); i++ {
					path = append(path, stack[i].String())
				}
				path = append(path, n.String())
			} else {
				path = []string{n.String()}
			}
			return fmt.Errorf("bootstrap: circular dependency detected: %s",
				strings.Join(path, " -> "))
		}
		if state[n] == 2 {
			return nil
		}
		state[n] = 1
		stack = append(stack, n)
		for _, nei := range graph[n] {
			if err := visit(nei); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = 2
		return nil
	}

	for node := range graph {
		if state[node] == 0 {
			if err := visit(node); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveByType returns a singleton instance for the given type.
// Checks direct bindings first, then the lazy-init provider cache.
func (c *Container) resolveByType(typ reflect.Type) (any, error) {
	// Check direct bindings (from Bind) — these are always preferred.
	c.mu.RLock()
	if val, ok := c.direct[typ]; ok {
		c.mu.RUnlock()
		return val, nil
	}
	// Check lazy singleton cache (from previous provider resolutions).
	if inst, ok := c.instances[typ]; ok {
		c.mu.RUnlock()
		return inst, nil
	}
	prov, ok := c.providers[typ]
	c.mu.RUnlock()

	if !ok {
		if c.strict {
			panic(fmt.Sprintf("bootstrap: service not found: %v", typ))
		}
		return nil, fmt.Errorf("bootstrap: service not found: %v", typ)
	}

	// Resolve each dependency recursively.
	args := make([]reflect.Value, len(prov.deps))
	for i, depType := range prov.deps {
		dep, err := c.resolveByType(depType)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: resolving dependency %v for %v: %w",
				depType, typ, err)
		}
		args[i] = reflect.ValueOf(dep)
	}

	// Call the provider function.
	outs := prov.fn.Call(args)
	if !outs[1].IsNil() {
		return nil, outs[1].Interface().(error)
	}
	inst := outs[0].Interface()

	// Store in lazy singleton cache (double-checked locking).
	c.mu.Lock()
	if existing, ok := c.instances[typ]; ok {
		c.mu.Unlock()
		return existing, nil // another goroutine beat us here
	}
	c.instances[typ] = inst
	c.mu.Unlock()
	return inst, nil
}

// Resolve retrieves a service by type. Returns an error if not found.
func Resolve[S any](c *Container) (S, error) {
	var zero S
	if c == nil {
		return zero, fmt.Errorf("bootstrap: Resolve called on nil container")
	}
	typ := reflect.TypeFor[S]()
	v, err := c.resolveByType(typ)
	if err != nil {
		return zero, err
	}
	s, ok := v.(S)
	if !ok {
		return zero, fmt.Errorf("bootstrap: type assertion failed: stored %T, requested %v", v, typ)
	}
	return s, nil
}

// MustResolve retrieves a service by type or panics if not found.
func MustResolve[S any](c *Container) S {
	val, err := Resolve[S](c)
	if err != nil {
		panic(err)
	}
	return val
}

// ResolveTransactor returns a database.Transactor for the named connection.
// Pass an empty string to use the primary connection.
func ResolveTransactor(c *Container, dbName string) (database.Transactor, error) {
	reg, err := Resolve[*database.Registry](c)
	if err != nil {
		return nil, err
	}
	return database.NewTransactorFromRegistry(reg, dbName)
}
