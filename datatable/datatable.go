package datatable

import (
	"context"
	"fmt"
	"strings"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/utils"
	"gorm.io/gorm"
)

type Datatable[T any] struct {
	rawDB            *gorm.DB // original unconditioned connection — used for author fetch
	db               *gorm.DB
	err              error
	dialect          string // "mysql" | "sqlite" | "postgres"
	tableName        string
	columns          map[string]bool
	queryParams      QueryParams
	searchableFields []string
	filters          []Filter
	views            []View
	authorExtractIDs func(row T) []int
	authorFetch      func(db *gorm.DB, ids []int) (any, error)
}

func New[T any](db *gorm.DB, queryParams QueryParams) *Datatable[T] {
	var model T
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&model); err != nil || stmt.Schema == nil {
		// Return a datatable in an error state; Get() will surface it.
		return &Datatable[T]{err: fmt.Errorf("datatable: failed to parse schema for %T: %v", model, err)}
	}

	tableName := stmt.Schema.Table
	dialect := db.Dialector.Name() // "mysql", "sqlite", "postgres", …

	return &Datatable[T]{
		rawDB:       db,
		db:          db,
		dialect:     dialect,
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
			finalColumns[i] = fmt.Sprintf("%s.%s", quoteIdent(d.dialect, d.tableName), quoteIdent(d.dialect, column))
		}
	}

	d.db = d.db.Select(finalColumns)
	return d
}

func (d *Datatable[T]) WithSearchableFields(fields []string) *Datatable[T] {
	d.searchableFields = fields
	return d
}

// WithDefaultOrder sets the fallback ordering used when the request does not
// specify an order column or direction. If the caller has already provided
// either value (via QueryParams), this method leaves those values unchanged.
func (d *Datatable[T]) WithDefaultOrder(column, direction string) *Datatable[T] {
	if d.queryParams.OrderCol == "" {
		d.queryParams.OrderCol = column
	}
	if d.queryParams.OrderDir == "" {
		d.queryParams.OrderDir = direction
	}
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

// WithViews registers multiple named query variants, appending to any views
// already registered via WithView. If the same URIKey appears more than once,
// the last registered view for that key wins (duplicate detection happens at
// Apply time — the first matching view in the slice is applied and the loop
// stops).
func (d *Datatable[T]) WithViews(views []View) *Datatable[T] {
	d.views = append(d.views, views...)
	return d
}

func (d *Datatable[T]) WithFilter(filter Filter) *Datatable[T] {
	d.filters = append(d.filters, filter)
	return d
}

// WithDateFilter registers a date-range filter for the given query parameter.
// The URI parameter value must be in one of the formats accepted by
// utils.GetDateRange (e.g. "2024-01-01,2024-12-31" or a named range like
// "this_month"). An empty or unrecognised value is a no-op.
//
// The optional dbColumn parameter specifies the actual database column name to
// filter on. When omitted, the query parameter name is used as the column name.
// Use this when the URL parameter name differs from the database column name,
// e.g. WithDateFilter("created_at", "datum_unos").
func (d *Datatable[T]) WithDateFilter(param string, dbColumn ...string) *Datatable[T] {
	dialect := d.dialect
	col := param
	if len(dbColumn) > 0 && dbColumn[0] != "" {
		col = dbColumn[0]
	}
	quotedCol := quoteIdent(dialect, col)
	dateFilter := NewFilter(param, func(query *gorm.DB, value string, tableName string) *gorm.DB {
		fromDate, toDate := utils.GetDateRange(value)

		if fromDate == "" || toDate == "" {
			return query
		}

		return query.Where(fmt.Sprintf("%s.%s BETWEEN ? AND ?", quoteIdent(dialect, tableName), quotedCol), fromDate, toDate)
	})
	d.filters = append(d.filters, dateFilter)
	return d
}

// WithStatusFilter adds a boolean status filter keyed by the given column.
//
// Deprecated: use WithFilter directly with your own status logic.
// Example:
//
//	WithFilter(datatable.NewFilter("status", func(q *gorm.DB, val, tbl string) *gorm.DB {
//	    if val == "active" { return q.Where(tbl+".status = ?", 1) }
//	    if val == "inactive" { return q.Where(tbl+".status = ?", 0) }
//	    return q
//	}))
func (d *Datatable[T]) WithStatusFilter(column string) *Datatable[T] {
	dialect := d.dialect
	quotedCol := quoteIdent(dialect, column)
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

		return query.Where(fmt.Sprintf("%s.%s = ?", quoteIdent(dialect, tableName), quotedCol), param)
	})
	d.filters = append(d.filters, statusFilter)
	return d
}

// WithScope applies additional query constraints to the datatable.
// The callback receives the current *gorm.DB and the table name.
// Any GORM methods called on the db (Where, Preload, Joins, etc.)
// are accumulated on the datatable's query builder.
// If called multiple times, constraints are additive.
//
// Tenant scoping example (use tenancy.RequireTenantScope for security-critical paths):
//
//	datatable.New[Vehicle](db, params).
//	    WithScope(func(q *gorm.DB, _ string) *gorm.DB {
//	        return q.Scopes(tenancy.ScopeByTenant(ctx, "dealer_id"))
//	    })
func (d *Datatable[T]) WithScope(callback func(query *gorm.DB, tableName string) *gorm.DB) *Datatable[T] {
	d.db = callback(d.db.Session(&gorm.Session{}), d.tableName)
	return d
}

// WithMapper is deprecated: transform rows in your handler or service layer
// instead of coupling presentation logic to the data-fetching layer.
func (d *Datatable[T]) WithMapper(_ func(*T) T) *Datatable[T] {
	return d
}

// WithAuthors enables author loading after rows are fetched.
//
// extractIDs is called for each fetched row and should return all relevant
// user IDs (e.g. created_by, updated_by). Duplicates and non-positive values
// are automatically filtered.
//
// fetch is called once with the deduplicated set of IDs to load the author
// records. The returned value is stored in result.Meta["authors"].
//
// Example:
//
//	WithAuthors(
//	    func(row Article) []int { return []int{row.CreatedBy, row.UpdatedBy} },
//	    func(db *gorm.DB, ids []int) (any, error) {
//	        var users []User
//	        return users, db.Select("id, name, avatar").Where("id IN ?", ids).Find(&users).Error
//	    },
//	)
func (d *Datatable[T]) WithAuthors(
	extractIDs func(row T) []int,
	fetch func(db *gorm.DB, ids []int) (any, error),
) *Datatable[T] {
	d.authorExtractIDs = extractIDs
	d.authorFetch = fetch
	return d
}

// TableName returns the resolved GORM table name for this datatable.
// Useful when building Filter or View callbacks outside the builder chain.
func (d *Datatable[T]) TableName() string {
	return d.tableName
}

// WithoutDeleted filters soft-deleted records using the given column name.
// For standard GORM soft-delete (deleted_at IS NULL), pass "deleted_at".
func (d *Datatable[T]) WithoutDeleted(column string) *Datatable[T] {
	d.db = d.db.Where(fmt.Sprintf("%s.%s IS NULL", quoteIdent(d.dialect, d.tableName), quoteIdent(d.dialect, column)))
	return d
}

func (d *Datatable[T]) Get(ctx context.Context) (*DatatableResult[T], error) {
	if d.err != nil {
		return nil, d.err
	}

	if d.columns == nil {
		return nil, fmt.Errorf("datatable[%s]: WithColumns must be called before Get()", d.tableName)
	}

	query := d.db.Session(&gorm.Session{}).WithContext(ctx)
	countQuery := d.db.Session(&gorm.Session{}).WithContext(ctx)

	// applyBoth applies fn to both the data query and the count query.
	applyBoth := func(fn func(*gorm.DB) *gorm.DB) {
		query = fn(query)
		countQuery = fn(countQuery)
	}

	quotedTable := quoteIdent(d.dialect, d.tableName)

	// Search
	// Supported field prefixes (all dialects supported):
	//   concat:col1,col2        — concatenate columns with a space separator
	//   concatws:SEP,col1,col2  — concatenate columns with a custom separator
	//   (plain)                 — single column
	if d.queryParams.Search != "" && len(d.searchableFields) > 0 {
		like := likeOp(d.dialect)
		words := strings.Fields(d.queryParams.Search)
		for _, word := range words {
			var wordConditions []string
			var values []any
			for _, field := range d.searchableFields {
				if after, ok := strings.CutPrefix(field, "concat:"); ok {
					cols := strings.Split(after, ",")
					trimmed := make([]string, len(cols))
					for i, c := range cols {
						trimmed[i] = strings.TrimSpace(c)
					}
					wordConditions = append(wordConditions, concatExpr(d.dialect, quotedTable, trimmed)+" "+like+" ?")
				} else if after, ok := strings.CutPrefix(field, "concatws:"); ok {
					// Format: concatws:SEPARATOR,col1,col2,...
					sepAndCols := strings.SplitN(after, ",", 2)
					if len(sepAndCols) == 2 {
						sep := sepAndCols[0]
						cols := strings.Split(sepAndCols[1], ",")
						trimmed := make([]string, len(cols))
						for i, c := range cols {
							trimmed[i] = strings.TrimSpace(c)
						}
						wordConditions = append(wordConditions, concatWSExpr(d.dialect, sep, quotedTable, trimmed)+" "+like+" ?")
					}
				} else {
					wordConditions = append(wordConditions, fmt.Sprintf("%s.%s %s ?", quotedTable, quoteIdent(d.dialect, field), like))
				}
				escaped := database.EscapeLike(word)
				values = append(values, "%"+escaped+"%")
			}
			clause := "(" + strings.Join(wordConditions, " OR ") + ")"
			applyBoth(func(q *gorm.DB) *gorm.DB {
				return q.Where(clause, values...)
			})
		}
	}

	// Filters
	for _, f := range d.filters {
		if val, exists := d.queryParams.Filters[f.URIKey]; exists && val != "" {
			applyBoth(func(q *gorm.DB) *gorm.DB {
				return f.Query(q, val, d.tableName)
			})
		}
	}

	// Apply views — only the first registered view matching the active key is applied.
	activeView := d.queryParams.View
	if activeView != "" {
		for _, view := range d.views {
			if view.URIKey != activeView {
				continue
			}
			applyBoth(func(q *gorm.DB) *gorm.DB {
				return view.Query(q, d.tableName)
			})
			break // first match wins; don't apply duplicate-key views
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

	query = query.Order(fmt.Sprintf("%s.%s %s", quotedTable, quoteIdent(d.dialect, d.queryParams.OrderCol), safeDirection))

	// Run COUNT and SELECT sequentially — both inherit context cancellation.
	var total int64
	data := make([]T, 0, d.queryParams.GetPerPage())

	var model T
	if err := countQuery.Model(&model).Count(&total).Error; err != nil {
		return nil, err
	}
	if err := query.
		Limit(d.queryParams.GetPerPage()).
		Offset((d.queryParams.GetPage() - 1) * d.queryParams.GetPerPage()).
		Find(&data).Error; err != nil {
		return nil, err
	}

	// Metadata
	var lastPage, from, to int
	if len(data) > 0 {
		lastPage = (int(total) + d.queryParams.GetPerPage() - 1) / d.queryParams.GetPerPage()
		from = (d.queryParams.GetPage()-1)*d.queryParams.GetPerPage() + 1
		to = from + len(data) - 1
	}

	// Authors: batch-load user records for all unique created_by/updated_by IDs.
	var meta map[string]any
	if d.authorExtractIDs != nil && d.authorFetch != nil {
		idSet := make(map[int]struct{}, len(data)*2)
		for _, row := range data {
			for _, id := range d.authorExtractIDs(row) {
				if id > 0 {
					idSet[id] = struct{}{}
				}
			}
		}
		if len(idSet) > 0 {
			ids := make([]int, 0, len(idSet))
			for id := range idSet {
				ids = append(ids, id)
			}
			authors, err := d.authorFetch(d.rawDB.WithContext(ctx), ids)
			if err != nil {
				return nil, err
			}
			meta = map[string]any{"authors": authors}
		}
	}

	return &DatatableResult[T]{
		Data:     data,
		Meta:     meta,
		Total:    total,
		PerPage:  d.queryParams.GetPerPage(),
		Page:     d.queryParams.GetPage(),
		LastPage: lastPage,
		From:     from,
		To:       to,
	}, nil
}
