// Package reflectioncache provides a concurrency-safe reflection metadata cache.
package reflectioncache

import (
	"reflect"
	"sync"
)

// FieldInfo contains metadata about a struct field. This is intentionally
// small — enough for callers that need field names, types and tags.
type FieldInfo struct {
	Name    string
	Type    reflect.Type
	Index   []int
	Tag     reflect.StructTag
	PkgPath string
}

type cacheEntry struct {
	fields []FieldInfo
}

// Cache is a concurrency-safe reflection metadata cache.
type Cache struct {
	mu sync.RWMutex
	m  map[reflect.Type]*cacheEntry
}

// New creates an empty Cache.
func New() *Cache {
	return &Cache{
		mu: sync.RWMutex{},
		m:  make(map[reflect.Type]*cacheEntry),
	}
}

// FieldsByType returns cached FieldInfo for the provided reflect.Type.
// Pointer types are normalized to their element type. Non-struct types
// return an empty slice (not nil) to simplify callers.
func (c *Cache) FieldsByType(t reflect.Type) []FieldInfo {
	if t == nil {
		return nil
	}
	// normalize pointer -> elem
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	// Fast path: read lock
	c.mu.RLock()

	if e, ok := c.m[t]; ok {
		result := make([]FieldInfo, len(e.fields))
		copy(result, e.fields)
		c.mu.RUnlock()
		return result
	}

	c.mu.RUnlock()

	// Build metadata
	var fields []FieldInfo

	if t.Kind() == reflect.Struct {
		n := t.NumField()

		fields = make([]FieldInfo, 0, n)
		for i := range n {
			field := t.Field(i)
			idx := make([]int, len(field.Index))
			copy(idx, field.Index)
			fields = append(fields, FieldInfo{
				Name:    field.Name,
				Type:    field.Type,
				Index:   idx,
				Tag:     field.Tag,
				PkgPath: field.PkgPath,
			})
		}
	} else {
		fields = []FieldInfo{}
	}

	// Store under write lock (double-check to avoid duplicate work)
	c.mu.Lock()

	if e, ok := c.m[t]; ok {
		fields = e.fields

		c.mu.Unlock()

		return fields
	}

	c.m[t] = &cacheEntry{fields: fields}
	c.mu.Unlock()

	result := make([]FieldInfo, len(fields))
	copy(result, fields)
	return result
}

// Fields returns cached FieldInfo for the dynamic value's type.
func (c *Cache) Fields(i any) []FieldInfo {
	return c.FieldsByType(reflect.TypeOf(i))
}

var defaultCache = New()

// FieldsByType is a package-level convenience function using a default cache.
func FieldsByType(t reflect.Type) []FieldInfo { return defaultCache.FieldsByType(t) }

// Fields is a package-level convenience function using a default cache.
func Fields(i any) []FieldInfo { return defaultCache.Fields(i) }
