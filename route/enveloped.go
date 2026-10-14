package route

import (
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/web"
)

// Enveloped is an output that answers go-core's envelope with a top-level meta next to the data:
// {"success":true,"data":Data(),"meta":Meta()}. Page and WithMeta implement it.
type Enveloped interface {
	Data() any
	Meta() any
}

// Page is a list output that answers exactly what web.Paginated writes: the rows as data and the
// paging numbers, with the result's own Meta merged in, as meta. A plain datatable.DatatableResult
// as output keeps its nested form.
type Page[T any] struct {
	Result datatable.DatatableResult[T]
}

// Data is the rows.
func (p Page[T]) Data() any { return p.Result.Data }

// Meta is the paging meta, as web.PageMeta builds it.
func (p Page[T]) Meta() any { return web.PageMeta(&p.Result) }

// WithMeta answers what web.JSON(c, status, Items, Info) writes.
type WithMeta[T, M any] struct {
	Items T
	Info  M
}

// Data is Items.
func (w WithMeta[T, M]) Data() any { return w.Items }

// Meta is Info.
func (w WithMeta[T, M]) Meta() any { return w.Info }
