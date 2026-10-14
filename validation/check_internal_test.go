//nolint:paralleltest,exhaustruct,noinlineerr,prealloc,varnamelen,exhaustruct_v5 // tests
package validation

import (
	"reflect"
	"testing"
)

func TestCheckRulesAcceptsEveryBuiltInRule(t *testing.T) {
	var fields []reflect.StructField

	i := 0
	for name := range defaultRegistry {
		fields = append(fields, reflect.StructField{
			Name: "F" + string(rune('A'+i/26)) + string(rune('A'+i%26)), Type: reflect.TypeFor[string](),
			Tag: reflect.StructTag(`validation:"` + name + `"`),
		})
		i++
	}

	if err := CheckRules(reflect.New(reflect.StructOf(fields)).Elem().Interface()); err != nil {
		t.Fatal(err)
	}
}
