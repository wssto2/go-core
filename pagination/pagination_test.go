package pagination_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wssto2/go-core/pagination"
)

func TestPageFilterValue(t *testing.T) {
	page := pagination.Page{Filters: map[string]string{"status": "open"}}

	got, ok := page.FilterValue("status")
	assert.True(t, ok)
	assert.Equal(t, "open", got)

	_, ok = page.FilterValue("missing")
	assert.False(t, ok)
}

func TestMapResultKeepsThePagingNumbers(t *testing.T) {
	in := &pagination.PageResult[int]{
		Items: []int{1, 2}, Meta: map[string]any{"k": "v"}, Views: []pagination.ViewCount{{Key: "all", Count: 7}},
		Total: 7, PerPage: 2, Page: 1, LastPage: 4, From: 1, To: 2,
	}

	out := pagination.MapResult(in, strconv.Itoa)

	assert.Equal(t, []string{"1", "2"}, out.Items)
	assert.Equal(t, in.Meta, out.Meta)
	assert.Equal(t, in.Views, out.Views)
	assert.Equal(t, int64(7), out.Total)
	assert.Equal(t, []int{2, 1, 4, 1, 2}, []int{out.PerPage, out.Page, out.LastPage, out.From, out.To})
	assert.False(t, out.IsEmpty())
	assert.True(t, (&pagination.PageResult[int]{}).IsEmpty())
}

func TestMapResourceKeepsTheMeta(t *testing.T) {
	out := pagination.MapResource(pagination.Resource[int]{Data: 5, Meta: map[string]any{"by": "x"}}, strconv.Itoa)

	assert.Equal(t, "5", out.Data)
	assert.Equal(t, map[string]any{"by": "x"}, out.Meta)
}
