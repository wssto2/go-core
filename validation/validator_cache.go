package validation

import (
	"reflect"
	"strings"
	"sync"

	"github.com/wssto2/go-core/internal/reflectioncache"
)

// cachedFieldMeta holds pre-computed validation metadata for a single exported
// struct field. It is immutable once built.
type cachedFieldMeta struct {
	GoName     string       // Go identifier (e.g. "PasswordConfirm")
	Alias      string       // resolved attribute name for error keys (form > json > GoName)
	FieldIndex []int        // reflect field index path for rv.FieldByIndex
	Rules      []parsedRule // nil if the field has no "validation" tag
	ElemType   reflect.Type // innermost struct type reachable via ptr/slice; nil if none
}

// cachedStructMeta holds pre-computed validation metadata for a struct type.
// It is immutable once built and safe for concurrent use.
type cachedStructMeta struct {
	// Fields contains only exported fields.
	Fields []cachedFieldMeta

	// HasAnyValidation is true when at least one field has validation rules or
	// leads to a nested struct (which may have rules). Used to short-circuit
	// recursion into types like time.Time that carry no validation at all.
	HasAnyValidation bool

	// NameLookup maps every valid identifier for a field (Go name, form tag,
	// json name) to the field's index in Fields. Used for O(1) cross-field
	// rule lookups (same, different, confirmed, required_if).
	NameLookup map[string]int
}

// structMetaCache is a concurrency-safe per-type metadata cache.
type structMetaCache struct {
	mu sync.RWMutex
	m  map[reflect.Type]*cachedStructMeta
}

// globalMetaCache is the package-level singleton. Metadata is computed once
// per struct type and reused for all subsequent Validate calls.
var globalMetaCache = &structMetaCache{
	m: make(map[reflect.Type]*cachedStructMeta),
}

// get returns the cached metadata for t, building and storing it on first access.
// Pointer types are normalised to their element type.
func (c *structMetaCache) get(t reflect.Type) *cachedStructMeta {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return &cachedStructMeta{}
	}

	c.mu.RLock()
	if meta, ok := c.m[t]; ok {
		c.mu.RUnlock()
		return meta
	}
	c.mu.RUnlock()

	meta := buildStructMeta(t)

	c.mu.Lock()
	if existing, ok := c.m[t]; ok {
		c.mu.Unlock()
		return existing
	}
	c.m[t] = meta
	c.mu.Unlock()

	return meta
}

// buildStructMeta constructs the metadata for t using reflectioncache to avoid
// re-running reflect.Type.Field() on subsequent calls.
func buildStructMeta(t reflect.Type) *cachedStructMeta {
	fiList := reflectioncache.FieldsByType(t)
	fields := make([]cachedFieldMeta, 0, len(fiList))
	nameLookup := make(map[string]int, len(fiList)*2)

	for _, fi := range fiList {
		if fi.PkgPath != "" {
			continue // unexported
		}

		alias := computeAliasFromTag(fi.Tag)
		if alias == "" {
			alias = fi.Name
		}

		var rules []parsedRule
		if tag := fi.Tag.Get("validation"); tag != "" {
			rules = parseValidationTag(tag)
		}

		// Determine the innermost struct type for recursive validation.
		elemType := innerStructType(fi.Type)

		idx := len(fields)
		fields = append(fields, cachedFieldMeta{
			GoName:     fi.Name,
			Alias:      alias,
			FieldIndex: fi.Index,
			Rules:      rules,
			ElemType:   elemType,
		})

		// Register all valid lookup names so cross-field rules can find this
		// field by any of its identifiers in O(1).
		nameLookup[fi.Name] = idx
		if alias != fi.Name {
			nameLookup[alias] = idx
		}
		// json name may differ from the chosen alias (when form tag takes precedence).
		if jsonTag := strings.TrimSpace(fi.Tag.Get("json")); jsonTag != "" && jsonTag != "-" {
			jsonName, _, _ := strings.Cut(jsonTag, ",")
			jsonName = strings.TrimSpace(jsonName)
			if jsonName != "" {
				if _, exists := nameLookup[jsonName]; !exists {
					nameLookup[jsonName] = idx
				}
			}
		}
	}

	hasAny := false
	for _, f := range fields {
		if len(f.Rules) > 0 || f.ElemType != nil {
			hasAny = true
			break
		}
	}

	return &cachedStructMeta{
		Fields:           fields,
		HasAnyValidation: hasAny,
		NameLookup:       nameLookup,
	}
}

// computeAliasFromTag resolves the preferred attribute name from struct tags.
// Priority: form tag > json tag (first segment) > empty string (caller falls
// back to the Go field name).
func computeAliasFromTag(tag reflect.StructTag) string {
	if form := strings.TrimSpace(tag.Get("form")); form != "" && form != "-" {
		return form
	}
	if jsonTag := strings.TrimSpace(tag.Get("json")); jsonTag != "" && jsonTag != "-" {
		name, _, _ := strings.Cut(jsonTag, ",")
		name = strings.TrimSpace(name)
		if name != "" {
			return name
		}
	}
	return ""
}

// innerStructType returns the innermost struct type reachable from t by
// dereferencing pointers and unwrapping slice/array element types.
// Returns nil when no struct type is reachable.
func innerStructType(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
	}
	if t.Kind() == reflect.Struct {
		return t
	}
	return nil
}
