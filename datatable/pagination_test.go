package datatable_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/pagination"
)

func TestParamsOf_CopiesEveryFieldUnclamped(t *testing.T) {
	got := datatable.ParamsOf(pagination.Page{
		Number: 0, Size: 100000, Search: "x", OrderCol: "name", OrderDir: "desc", View: "mine",
		Filters: map[string]string{"status": "1"},
	})

	assert.Equal(t, datatable.QueryParams{
		View: "mine", Page: 0, PerPage: 100000, Search: "x", OrderCol: "name", OrderDir: "desc",
		Filters: map[string]string{"status": "1"},
	}, got)
}

func TestToPageResult_CarriesTheRowsAndTheNumbers(t *testing.T) {
	res := &datatable.DatatableResult[string]{
		Data: []string{"a", "b"}, Meta: map[string]any{"k": 1}, Total: 12, PerPage: 2, Page: 3, LastPage: 6, From: 5, To: 6,
	}

	assert.Equal(t, &pagination.PageResult[string]{
		Items: []string{"a", "b"}, Meta: map[string]any{"k": 1}, Total: 12, PerPage: 2, Page: 3, LastPage: 6, From: 5, To: 6,
	}, res.ToPageResult())
}
