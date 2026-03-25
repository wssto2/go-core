package datatable

import (
	"fmt"
	"strings"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/utils"
	"gorm.io/gorm"
)

type Datatable[T any] struct {
	db               *gorm.DB
	err              error
	tableName        string
	columns          map[string]bool
	queryParams      QueryParams
	searchableFields []string
	filters          []Filter
	views            []View
	mapper           func(*T) T
}

func New[T any](db *gorm.DB, queryParams QueryParams) *Datatable[T] {
	var model T
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&model); err != nil || stmt.Schema == nil {
		// Return a datatable in an error state; Get() will surface it.
		return &Datatable[T]{err: fmt.Errorf("datatable: failed to parse schema for %T: %v", model, err)}
	}

	tableName := stmt.Schema.Table

	return &Datatable[T]{
		db:          db,
		queryParams: queryParams,
		tableName:   tableName,
	}
}

func (d *Datatable[T]) WithColumns(columns []string) *Datatable[T] {
	if d.columns == nil {
		d.columns = make(map[string]bool, len(columns))
	}

	finalColumns := make([]string, len(columns))
	for i, column := range columns {
		// Track the bare column name (without table prefix) as allowed
		d.columns[column] = true

		if strings.Contains(column, ".") || strings.Contains(column, "(") {
			finalColumns[i] = column
		} else {
			finalColumns[i] = fmt.Sprintf("%s.%s", d.tableName, column)
		}
	}

	d.db = d.db.Select(finalColumns)
	return d
}

func (d *Datatable[T]) WithSearchableFields(fields []string) *Datatable[T] {
	d.searchableFields = fields
	return d
}

func (d *Datatable[T]) WithDefaultOrder(column, direction string) *Datatable[T] {
	d.queryParams.OrderCol = column
	d.queryParams.OrderDir = direction
	return d
}

// WithView registers a named query variant that is activated when the request
// contains view=<uriKey> in the query string. Multiple views can be registered;
// only the one matching the active view key is applied.
func (d *Datatable[T]) WithView(uriKey string, callback func(query *gorm.DB, tableName string) *gorm.DB) *Datatable[T] {
	view := View{
		URIKey: uriKey,
		Query:  callback,
	}

	d.views = append(d.views, view)

	return d
}

// WithViews registers multiple views at once.
func (d *Datatable[T]) WithViews(views []View) *Datatable[T] {
	d.views = views

	return d
}

func (d *Datatable[T]) WithFilter(filter Filter) *Datatable[T] {
	d.filters = append(d.filters, filter)
	return d
}

// WithDateFilter
func (d *Datatable[T]) WithDateFilter(column string) *Datatable[T] {
	dateFilter := NewFilter(column, func(query *gorm.DB, value string, tableName string) *gorm.DB {
		fromDate, toDate := utils.GetDateRange(value)

		if fromDate == "" || toDate == "" {
			return query
		}

		return query.Where(fmt.Sprintf("%s.%s BETWEEN ? AND ?", tableName, column), fromDate, toDate)
	})
	d.filters = append(d.filters, dateFilter)
	return d
}

// WithStatusFilter
func (d *Datatable[T]) WithStatusFilter(column string) *Datatable[T] {
	statusFilter := NewFilter(column, func(query *gorm.DB, value string, tableName string) *gorm.DB {
		param := ""

		switch value {
		case "active":
			param = "1"
		case "inactive":
			param = "0"
		}

		if param == "" {
			return query
		}

		return query.Where(fmt.Sprintf("%s.%s = ?", tableName, column), param)
	})
	d.filters = append(d.filters, statusFilter)
	return d
}

// WithQuery applies additional query constraints to the datatable.
// The callback receives the current *gorm.DB and the table name.
// Any GORM methods called on the db (Where, Preload, Joins, etc.)
// are accumulated on the datatable's query builder.
// If called multiple times, constraints are additive.
func (d *Datatable[T]) WithQuery(callback func(query *gorm.DB, tableName string) *gorm.DB) *Datatable[T] {
	d.db = callback(d.db.Session(&gorm.Session{}), d.tableName)
	return d
}

// WithMapper
func (d *Datatable[T]) WithMapper(mapper func(*T) T) *Datatable[T] {
	d.mapper = mapper
	return d
}

// WithoutDeleted filters soft-deleted records using the given column name.
// For standard GORM soft-delete (deleted_at IS NULL), pass "deleted_at".
func (d *Datatable[T]) WithoutDeleted(column string) *Datatable[T] {
	d.db = d.db.Where(fmt.Sprintf("%s.%s IS NULL", d.tableName, database.EscapeColumn(column)))
	return d
}

func (d *Datatable[T]) Get() (*DatatableResult[T], error) {
	if d.err != nil {
		return nil, d.err
	}

	if d.columns == nil {
		return nil, fmt.Errorf("datatable[%s]: WithColumns must be called before Get()", d.tableName)
	}

	query := d.db.Session(&gorm.Session{})
	countQuery := d.db.Session(&gorm.Session{})

	// Search
	if d.queryParams.Search != "" && len(d.searchableFields) > 0 {
		words := strings.Fields(d.queryParams.Search)
		for _, word := range words {
			var wordConditions []string
			var values []any
			for _, field := range d.searchableFields {
				if after, ok := strings.CutPrefix(field, "concat:"); ok {
					cols := strings.Split(after, ",")
					var concatExpr []string
					for _, column := range cols {
						concatExpr = append(concatExpr, fmt.Sprintf("IFNULL(%s.%s, '')", d.tableName, database.EscapeColumn(column)))
					}
					wordConditions = append(wordConditions, "CONCAT("+strings.Join(concatExpr, ", ' ', ")+") LIKE ?")
				} else {
					wordConditions = append(wordConditions, fmt.Sprintf("%s.%s LIKE ?", d.tableName, database.EscapeColumn(field)))
				}
				escaped := database.EscapeLike(word)
				values = append(values, "%"+escaped+"%")
			}
			sql := "(" + strings.Join(wordConditions, " OR ") + ")"
			query = query.Where(sql, values...)
			countQuery = countQuery.Where(sql, values...)
		}
	}

	// Filters
	for _, f := range d.filters {
		if val, exists := d.queryParams.Filters[f.URIKey]; exists && val != "" {
			query = f.Query(query, val, d.tableName)
			countQuery = f.Query(countQuery, val, d.tableName)
		}
	}

	// Apply views
	activeView := d.queryParams.View
	if activeView != "" {
		for _, view := range d.views {
			if view.URIKey != activeView {
				continue
			}

			// Apply view query
			query = view.Query(query, d.tableName)
			countQuery = view.Query(countQuery, d.tableName)
		}
	}

	// Order
	var safeDirection string
	if strings.ToLower(d.queryParams.OrderDir) == "asc" {
		safeDirection = "ASC"
	} else {
		safeDirection = "DESC"
	}

	// Validate orderCol against allowed columns whitelist
	_, allowed := d.columns[d.queryParams.OrderCol]
	if !allowed {
		d.queryParams.OrderCol = "id" // fallback to default
	}

	query = query.Order(fmt.Sprintf("%s.%s %s", d.tableName, d.queryParams.OrderCol, safeDirection))

	var total int64
	data := make([]T, 0, d.queryParams.GetPerPage())
	var lastPage int
	var from, to int

	// Count
	var model T
	if err := countQuery.Model(&model).Count(&total).Error; err != nil {
		return nil, err
	}

	// Paginate
	if err := query.Limit(d.queryParams.GetPerPage()).Offset((d.queryParams.GetPage() - 1) * d.queryParams.GetPerPage()).Find(&data).Error; err != nil {
		return nil, err
	}

	// Metadata
	if len(data) > 0 {
		lastPage = (int(total) + d.queryParams.GetPerPage() - 1) / d.queryParams.GetPerPage()
		from = (d.queryParams.GetPage()-1)*d.queryParams.GetPerPage() + 1
		to = from + len(data) - 1
	}

	return &DatatableResult[T]{
		Data:     data,
		Meta:     make(map[string]any),
		Total:    total,
		PerPage:  d.queryParams.GetPerPage(),
		Page:     d.queryParams.GetPage(),
		LastPage: lastPage,
		From:     from,
		To:       to,
	}, nil
}
