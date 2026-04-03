package reflectioncache

import (
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inner struct {
	X int `json:"x"`
}

type S struct {
	A string
	B int `custom:"yes"`
	C inner
	D *inner
	e int // unexported
}

func TestFieldsBasic(t *testing.T) {
	fs := FieldsByType(reflect.TypeOf(S{}))
	if len(fs) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(fs))
	}
	if fs[0].Name != "A" || fs[1].Name != "B" || fs[2].Name != "C" || fs[3].Name != "D" || fs[4].Name != "e" {
		t.Fatalf("unexpected field names: %#v", fs)
	}
	if got := fs[1].Tag.Get("custom"); got != "yes" {
		t.Fatalf("expected tag custom=\"yes\", got %q", got)
	}

	// pointer normalization
	fs2 := FieldsByType(reflect.TypeOf(&S{}))
	if len(fs2) != 5 {
		t.Fatalf("expected 5 fields for pointer, got %d", len(fs2))
	}
}

func TestFieldsNonStruct(t *testing.T) {
	fs := FieldsByType(reflect.TypeOf(123))
	if len(fs) != 0 {
		t.Fatalf("expected 0 for non-struct, got %d", len(fs))
	}
}

func TestFieldsConcurrency(t *testing.T) {
	typ := reflect.TypeOf(S{})
	var wg sync.WaitGroup
	goroutines := 50
	loops := 1000
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < loops; j++ {
				_ = FieldsByType(typ)
			}
		}()
	}
	wg.Wait()
}

func TestCache_ReturnedSlice_IsIndependent(t *testing.T) {
type item struct {
A string
B int
}

c := New()
typ := reflect.TypeOf(item{})

first := c.FieldsByType(typ)
require.Len(t, first, 2)

// Sort the returned slice in reverse order.
first[0], first[1] = first[1], first[0]

// A second call must return the original order, not the mutated one.
second := c.FieldsByType(typ)
require.Len(t, second, 2)
assert.Equal(t, "A", second[0].Name, "cache must not be affected by caller mutation")
assert.Equal(t, "B", second[1].Name)
}
