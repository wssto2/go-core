package binders_test

import (
	"fmt"

	"github.com/wssto2/go-core/binders"
)

func ExampleBindStrings() {
	type input struct {
		ID   int      `path:"id"`
		Tags []string `path:"tag"`
		Page int      `path:"page"`
	}

	var in input
	err := binders.BindStrings(&in, "path", map[string][]string{
		"id":  {"7"},
		"tag": {"a", "b"},
	})
	fmt.Println(in.ID, in.Tags, in.Page, err)

	err = binders.BindStrings(&in, "path", map[string][]string{"id": {"seven"}})
	fmt.Println(err != nil)
	// Output:
	// 7 [a b] 0 <nil>
	// true
}
