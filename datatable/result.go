package datatable

// Meta carries optional enrichment data populated by features like WithAuthors.
// Absent from JSON responses when empty (omitempty).
type DatatableResult[T any] struct {
	Data     []T            `json:"data"`
	Meta     map[string]any `json:"meta,omitempty"`
	Total    int64          `json:"total"`
	PerPage  int            `json:"per_page"`
	Page     int            `json:"current_page"`
	LastPage int            `json:"last_page"`
	From     int            `json:"from"`
	To       int            `json:"to"`
}

func (d *DatatableResult[T]) IsEmpty() bool {
	return len(d.Data) == 0
}
