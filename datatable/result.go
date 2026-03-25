package datatable

type DatatableResult[T any] struct {
	Data     []T            `json:"data"`
	Meta     map[string]any `json:"meta"`
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
