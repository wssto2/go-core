package datatable_test

import (
	"encoding/json"
	"fmt"

	"github.com/wssto2/go-core/datatable"
)

// A list with tabs puts the count of each view in its meta, in one shape for every list.
func ExampleDatatableResult_WithViews() {
	page := datatable.DatatableResult[string]{Data: []string{"ana"}, Total: 1}.WithViews(
		datatable.ViewCount{Key: "active", Count: 7},
		datatable.ViewCount{Key: "all", Count: 9},
	)

	out, _ := json.Marshal(page.Meta)
	fmt.Println(string(out))
	// Output: {"views":[{"key":"active","count":7},{"key":"all","count":9}]}
}

func ExampleViewCount() {
	out, _ := json.Marshal(datatable.ViewCount{Key: "failed", Count: 2})
	fmt.Println(string(out))
	// Output: {"key":"failed","count":2}
}
