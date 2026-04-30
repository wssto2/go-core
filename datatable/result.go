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

// Map transforms the Data rows of a DatatableResult using fn, preserving all
// pagination metadata. Use this to convert generated database row types (which
// use DB column names as JSON keys) into domain entity types with clean API
// field names — without touching the datatable query layer.
//
// Example:
//
//	result, err := datatable.New[LocaleRow](db, params).WithColumns(...).Get(ctx)
//	return datatable.Map(result, func(r LocaleRow) Locale {
//	    return Locale{ID: r.ID, Code: r.Oznaka, Name: r.Naziv}
//	})
func Map[TFrom, TTo any](result *DatatableResult[TFrom], fn func(TFrom) TTo) *DatatableResult[TTo] {
	mapped := make([]TTo, len(result.Data))
	for i, row := range result.Data {
		mapped[i] = fn(row)
	}
	return &DatatableResult[TTo]{
		Data:     mapped,
		Meta:     result.Meta,
		Total:    result.Total,
		PerPage:  result.PerPage,
		Page:     result.Page,
		LastPage: result.LastPage,
		From:     result.From,
		To:       result.To,
	}
}
