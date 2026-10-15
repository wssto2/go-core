// Package pagination holds the framework-free paging primitives a domain or application layer can return and take:
// the page a listing asks for, the page it answers and a single record with its meta. An adapter translates them to the
// transport (web.WritePage, typed.ToPage) or the query builder (datatable), so no layer above the adapter imports either.
package pagination

// Page describes the paging, ordering, search and named selections of a listing query, with no query-builder or
// transport types. The request's filter and view selections are plain strings; the repository adapter maps those keys
// to predicates.
type Page struct {
	Number   int               // 1-based page number
	Size     int               // requested page size
	Search   string            // free-text search term
	OrderCol string            // requested order column (a whitelisted key)
	OrderDir string            // "asc" or "desc"
	View     string            // selected named view, if any
	Filters  map[string]string // request filter key to requested value
}

// FilterValue returns the requested value of a filter key and whether the request had it.
func (p Page) FilterValue(key string) (string, bool) {
	v, ok := p.Filters[key]

	return v, ok
}

// ViewCount is one scope of a list and how many rows it holds.
type ViewCount struct {
	Key   string
	Count int64
}

// PageResult is a page of T with its paging numbers.
type PageResult[T any] struct {
	Items []T
	Meta  map[string]any
	// Views are the list's scopes with their counts, in the order they are offered; empty for a list without counted scopes.
	Views    []ViewCount
	Total    int64
	PerPage  int
	Page     int
	LastPage int
	From     int
	To       int
}

// IsEmpty reports whether the result carries no rows.
func (r *PageResult[T]) IsEmpty() bool {
	return len(r.Items) == 0
}

// MapResult converts the items of a PageResult with fn and keeps the paging numbers, so an adapter's row type becomes
// the caller's without running the query again.
func MapResult[TFrom, TTo any](result *PageResult[TFrom], fn func(TFrom) TTo) *PageResult[TTo] {
	items := make([]TTo, len(result.Items))
	for i, row := range result.Items {
		items[i] = fn(row)
	}

	return &PageResult[TTo]{
		Items:    items,
		Meta:     result.Meta,
		Views:    result.Views,
		Total:    result.Total,
		PerPage:  result.PerPage,
		Page:     result.Page,
		LastPage: result.LastPage,
		From:     result.From,
		To:       result.To,
	}
}

// Resource is a single record with the response meta that goes with it (the people behind it, related counts).
type Resource[T any] struct {
	Data T              `json:"data"`
	Meta map[string]any `json:"meta"`
}

// MapResource converts the Data of a Resource with fn and keeps its Meta.
func MapResource[TFrom, TTo any](r Resource[TFrom], fn func(TFrom) TTo) Resource[TTo] {
	return Resource[TTo]{Data: fn(r.Data), Meta: r.Meta}
}
