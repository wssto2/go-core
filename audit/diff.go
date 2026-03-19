// audit/diff.go
package audit

import (
	"reflect"
	"strings"
)

// Diff returns the field names that differ between two structs.
// Both must be the same type. Uses json tags as field names.
func Diff(before, after any) []string {
	var changed []string

	bv := reflect.ValueOf(before)
	av := reflect.ValueOf(after)
	bt := reflect.TypeOf(before)

	for i := range bt.NumField() {
		if !reflect.DeepEqual(bv.Field(i).Interface(), av.Field(i).Interface()) {
			tag := bt.Field(i).Tag.Get("json")
			if tag == "" || tag == "-" {
				tag = bt.Field(i).Name
			}
			// strip omitempty etc
			if idx := strings.Index(tag, ","); idx != -1 {
				tag = tag[:idx]
			}
			changed = append(changed, tag)
		}
	}

	return changed
}
