package datatable

import "github.com/wssto2/go-core/pagination"

// ParamsOf converts the Page a repository port takes into the QueryParams the builder takes. The conversion is
// lossless and unclamped: the builder clamps the page and the size itself.
func ParamsOf(page pagination.Page) QueryParams {
	return QueryParams{
		View:     page.View,
		Page:     page.Number,
		PerPage:  page.Size,
		Search:   page.Search,
		OrderCol: page.OrderCol,
		OrderDir: page.OrderDir,
		Filters:  page.Filters,
	}
}

// ToPageResult converts the result of a query into the pagination.PageResult a repository port returns, so the layers
// above need not import datatable.
func (d *DatatableResult[T]) ToPageResult() *pagination.PageResult[T] {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design: a datatable has no counted views
	return &pagination.PageResult[T]{
		Items:    d.Data,
		Meta:     d.Meta,
		Total:    d.Total,
		PerPage:  d.PerPage,
		Page:     d.Page,
		LastPage: d.LastPage,
		From:     d.From,
		To:       d.To,
	}
}
