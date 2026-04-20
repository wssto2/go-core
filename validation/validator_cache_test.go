package validation

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// freshCache returns a structMetaCache that is independent of the global one,
// so tests don't interfere with each other or the package-level singleton.
func freshCache() *structMetaCache {
	return &structMetaCache{m: make(map[reflect.Type]*cachedStructMeta)}
}

// ─────────────────────────────────────────────────────────────────────────────
// computeAliasFromTag
// ─────────────────────────────────────────────────────────────────────────────

func TestComputeAliasFromTag_FormTagTakesPriority(t *testing.T) {
	type S struct {
		Field string `form:"myform" json:"myjson"`
	}
	rt := reflect.TypeOf(S{})
	tag := rt.Field(0).Tag
	alias := computeAliasFromTag(tag)
	if alias != "myform" {
		t.Fatalf("expected 'myform', got %q", alias)
	}
}

func TestComputeAliasFromTag_JsonTagUsedWhenNoForm(t *testing.T) {
	type S struct {
		Field string `json:"myjson,omitempty"`
	}
	rt := reflect.TypeOf(S{})
	alias := computeAliasFromTag(rt.Field(0).Tag)
	if alias != "myjson" {
		t.Fatalf("expected 'myjson', got %q", alias)
	}
}

func TestComputeAliasFromTag_DashFormIgnored(t *testing.T) {
	type S struct {
		Field string `form:"-" json:"myjson"`
	}
	rt := reflect.TypeOf(S{})
	alias := computeAliasFromTag(rt.Field(0).Tag)
	if alias != "myjson" {
		t.Fatalf("expected 'myjson', got %q", alias)
	}
}

func TestComputeAliasFromTag_EmptyTagsReturnsEmpty(t *testing.T) {
	type S struct {
		Field string
	}
	rt := reflect.TypeOf(S{})
	alias := computeAliasFromTag(rt.Field(0).Tag)
	if alias != "" {
		t.Fatalf("expected empty string, got %q", alias)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// innerStructType
// ─────────────────────────────────────────────────────────────────────────────

func TestInnerStructType_PlainStruct(t *testing.T) {
	type Inner struct{}
	got := innerStructType(reflect.TypeOf(Inner{}))
	if got != reflect.TypeOf(Inner{}) {
		t.Fatalf("unexpected type: %v", got)
	}
}

func TestInnerStructType_PtrToStruct(t *testing.T) {
	type Inner struct{}
	var v *Inner
	got := innerStructType(reflect.TypeOf(v))
	if got != reflect.TypeOf(Inner{}) {
		t.Fatalf("unexpected type: %v", got)
	}
}

func TestInnerStructType_SliceOfStruct(t *testing.T) {
	type Inner struct{}
	got := innerStructType(reflect.TypeOf([]Inner{}))
	if got != reflect.TypeOf(Inner{}) {
		t.Fatalf("unexpected type: %v", got)
	}
}

func TestInnerStructType_SliceOfPtrToStruct(t *testing.T) {
	type Inner struct{}
	got := innerStructType(reflect.TypeOf([]*Inner{}))
	if got != reflect.TypeOf(Inner{}) {
		t.Fatalf("unexpected type: %v", got)
	}
}

func TestInnerStructType_StringReturnsNil(t *testing.T) {
	got := innerStructType(reflect.TypeOf(""))
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestInnerStructType_SliceOfStringReturnsNil(t *testing.T) {
	got := innerStructType(reflect.TypeOf([]string{}))
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildStructMeta
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildStructMeta_AliasResolution(t *testing.T) {
	type S struct {
		GoOnly  string `validation:"required"`
		WithJSON string `json:"with_json" validation:"required"`
		WithForm string `form:"with_form" json:"with_json2" validation:"required"`
	}

	meta := buildStructMeta(reflect.TypeOf(S{}))

	aliases := make(map[string]string)
	for _, f := range meta.Fields {
		aliases[f.GoName] = f.Alias
	}

	if aliases["GoOnly"] != "GoOnly" {
		t.Errorf("GoOnly: expected 'GoOnly', got %q", aliases["GoOnly"])
	}
	if aliases["WithJSON"] != "with_json" {
		t.Errorf("WithJSON: expected 'with_json', got %q", aliases["WithJSON"])
	}
	if aliases["WithForm"] != "with_form" {
		t.Errorf("WithForm: expected 'with_form', got %q", aliases["WithForm"])
	}
}

func TestBuildStructMeta_UnexportedFieldsExcluded(t *testing.T) {
	type S struct {
		Exported   string `validation:"required"`
		unexported string //nolint:unused
	}

	meta := buildStructMeta(reflect.TypeOf(S{}))
	for _, f := range meta.Fields {
		if f.GoName == "unexported" {
			t.Fatal("unexported field should not be included in cache")
		}
	}
}

func TestBuildStructMeta_RulesParsed(t *testing.T) {
	type S struct {
		Email string `json:"email" validation:"required|email"`
	}

	meta := buildStructMeta(reflect.TypeOf(S{}))
	if len(meta.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(meta.Fields))
	}

	rules := meta.Fields[0].Rules
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].name != "required" || rules[1].name != "email" {
		t.Errorf("unexpected rules: %+v", rules)
	}
}

func TestBuildStructMeta_HasAnyValidation_True(t *testing.T) {
	type S struct {
		Name string `validation:"required"`
	}
	meta := buildStructMeta(reflect.TypeOf(S{}))
	if !meta.HasAnyValidation {
		t.Fatal("expected HasAnyValidation=true")
	}
}

func TestBuildStructMeta_HasAnyValidation_False(t *testing.T) {
	type S struct {
		Name string `json:"name"`
	}
	meta := buildStructMeta(reflect.TypeOf(S{}))
	if meta.HasAnyValidation {
		t.Fatal("expected HasAnyValidation=false for struct with no validation tags")
	}
}

func TestBuildStructMeta_HasAnyValidation_TrueWhenNestedStructPresent(t *testing.T) {
	type Inner struct {
		Value string `validation:"required"`
	}
	type Outer struct {
		Inner Inner
	}
	meta := buildStructMeta(reflect.TypeOf(Outer{}))
	if !meta.HasAnyValidation {
		t.Fatal("expected HasAnyValidation=true because Inner has validation rules")
	}
}

func TestBuildStructMeta_NameLookup_AllNamesRegistered(t *testing.T) {
	type S struct {
		EmailAddr string `form:"email_form" json:"email_json" validation:"required"`
	}
	meta := buildStructMeta(reflect.TypeOf(S{}))

	for _, name := range []string{"EmailAddr", "email_form", "email_json"} {
		if _, ok := meta.NameLookup[name]; !ok {
			t.Errorf("expected %q in NameLookup", name)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// structMetaCache
// ─────────────────────────────────────────────────────────────────────────────

func TestStructMetaCache_ReturnsSameInstanceOnRepeatCall(t *testing.T) {
	c := freshCache()
	type S struct {
		Name string `validation:"required"`
	}
	t1 := c.get(reflect.TypeOf(S{}))
	t2 := c.get(reflect.TypeOf(S{}))
	if t1 != t2 {
		t.Fatal("expected same *cachedStructMeta pointer on repeat call")
	}
}

func TestStructMetaCache_PtrNormalisedToElem(t *testing.T) {
	c := freshCache()
	type S struct {
		Name string `validation:"required"`
	}
	direct := c.get(reflect.TypeOf(S{}))
	viaPtr := c.get(reflect.TypeOf(&S{}))
	if direct != viaPtr {
		t.Fatal("expected ptr and non-ptr to resolve to the same cache entry")
	}
}

func TestStructMetaCache_NonStructReturnsEmptyMeta(t *testing.T) {
	c := freshCache()
	meta := c.get(reflect.TypeOf("hello"))
	if meta == nil {
		t.Fatal("expected non-nil empty meta")
	}
	if len(meta.Fields) != 0 {
		t.Fatalf("expected 0 fields, got %d", len(meta.Fields))
	}
}

func TestStructMetaCache_ConcurrentReadsAndWrites(t *testing.T) {
	c := freshCache()
	type S struct {
		Name string `validation:"required"`
	}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			meta := c.get(reflect.TypeOf(S{}))
			if meta == nil || len(meta.Fields) == 0 {
				t.Errorf("got unexpected meta: %+v", meta)
			}
		}()
	}
	wg.Wait()
}

// ─────────────────────────────────────────────────────────────────────────────
// stdlib type short-circuit
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildStructMeta_StdlibTime_NoValidation(t *testing.T) {
	meta := buildStructMeta(reflect.TypeOf(time.Time{}))
	if meta.HasAnyValidation {
		t.Fatal("time.Time should not have any validation")
	}
}
