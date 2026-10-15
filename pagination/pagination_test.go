package pagination_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wssto2/go-core/pagination"
)

func TestPageFilterValue(t *testing.T) {
	t.Parallel()

	page := pagination.Page{
		Number: 0, Size: 0, Search: "", OrderCol: "", OrderDir: "", View: "", Filters: map[string]string{"status": "open"},
	}

	got, ok := page.FilterValue("status")
	assert.True(t, ok)
	assert.Equal(t, "open", got)

	_, ok = page.FilterValue("missing")
	assert.False(t, ok)
}

func TestMapResultKeepsThePagingNumbers(t *testing.T) {
	t.Parallel()

	source := &pagination.PageResult[int]{
		Items: []int{1, 2}, Meta: map[string]any{"k": "v"}, Views: []pagination.ViewCount{{Key: "all", Count: 7}},
		Total: 7, PerPage: 2, Page: 1, LastPage: 4, From: 1, To: 2,
	}

	out := pagination.MapResult(source, strconv.Itoa)

	assert.Equal(t, []string{"1", "2"}, out.Items)
	assert.Equal(t, source.Meta, out.Meta)
	assert.Equal(t, source.Views, out.Views)
	assert.Equal(t, int64(7), out.Total)
	assert.Equal(t, []int{2, 1, 4, 1, 2}, []int{out.PerPage, out.Page, out.LastPage, out.From, out.To})
	assert.False(t, out.IsEmpty())
	assert.True(t, new(pagination.PageResult[int]).IsEmpty())
}

func TestMapResourceKeepsTheMeta(t *testing.T) {
	t.Parallel()

	out := pagination.MapResource(pagination.Resource[int]{Data: 5, Meta: map[string]any{"by": "x"}}, strconv.Itoa)

	assert.Equal(t, "5", out.Data)
	assert.Equal(t, map[string]any{"by": "x"}, out.Meta)
}
