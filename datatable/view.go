package datatable

import "gorm.io/gorm"

// ViewCount is how many rows one view of a list has: the entry of a list's meta.views, which a
// client shows as the badge of a tab. Every list with tab counts speaks this shape, so a client
// reads them all alike. The key is the view's own (the one the "view" parameter takes).
type ViewCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// WithViews puts the counts of the list's views in its meta as "views", keeping what else meta
// holds, and returns the result.
func (d DatatableResult[T]) WithViews(counts ...ViewCount) DatatableResult[T] {
	meta := make(map[string]any, len(d.Meta)+1)
	for k, v := range d.Meta {
		meta[k] = v
	}

	meta["views"] = counts
	d.Meta = meta

	return d
}

type View struct {
	URIKey string
	Query  func(query *gorm.DB, tableName string) *gorm.DB
}

func NewView(key string, fn func(q *gorm.DB, t string) *gorm.DB) View {
	return View{URIKey: key, Query: fn}
}
