package resource

import (
	"fmt"
	"reflect"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/types"
	"gorm.io/gorm"
)

type AuthorLoader func(db *gorm.DB, ids []int) ([]any, error)

type Count struct {
	table      string
	count      int64
	foreignKey string
	clause     string
}

type Resource[T any] struct {
	db           *gorm.DB
	counts       []Count
	tableName    string
	authorLoader AuthorLoader
	authorColumn string
	editorColumn string
	err          error
}

type Response[T any] struct {
	Data T              `json:"data"`
	Meta map[string]any `json:"meta"`
}

type authorIDs struct {
	creatorID int
	editorID  int
}

func New[T any](db *gorm.DB) *Resource[T] {
	var model T
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&model); err != nil || stmt.Schema == nil {
		return &Resource[T]{db: db, err: fmt.Errorf("resource: failed to parse schema for %T: %v", model, err)}
	}
	return &Resource[T]{db: db, tableName: stmt.Schema.Table}
}

func (r *Resource[T]) WithAuthorLoader(authorField, editorField string, loader AuthorLoader) *Resource[T] {
	r.authorColumn = authorField
	r.editorColumn = editorField
	r.authorLoader = loader
	return r
}

// WithQuery.
func (r *Resource[T]) WithQuery(callback func(query *gorm.DB, tableName string) *gorm.DB) *Resource[T] {
	r.db = callback(r.db.Session(&gorm.Session{}), r.tableName)

	return r
}

// WithCount adds a sub-count to the response Meta.
// tableName and foreignKey must be hardcoded literals, never user-provided.
// clause is raw SQL appended as an additional WHERE condition — treat it
// as a trusted, hardcoded filter (e.g. "active = 1"), never user input.
func (r *Resource[T]) WithCount(tableName string, foreignKey string, clause string) *Resource[T] {
	if tableName == "" || foreignKey == "" {
		r.err = fmt.Errorf("resource.WithCount: tableName and foreignKey must not be empty")
		return r
	}

	r.counts = append(r.counts, Count{
		table:      tableName,
		foreignKey: foreignKey,
		clause:     clause,
	})

	return r
}

// WithoutDeleted filters soft-deleted records using the given column name.
// For standard GORM soft-delete (deleted_at IS NULL), pass "deleted_at".
func (r *Resource[T]) WithoutDeleted(column string) *Resource[T] {
	r.db = r.db.Where(fmt.Sprintf("%s.%s IS NULL", r.tableName, database.EscapeColumn(column)))
	return r
}

func (r *Resource[T]) FindByID(id int) (Response[T], error) {
	if r.err != nil {
		return Response[T]{}, r.err
	}

	var result T
	var response Response[T]

	response.Meta = make(map[string]any)

	if err := r.db.First(&result, id).Error; err != nil {
		return response, err
	}

	// Get Created By and Updated By and include them in the result
	if r.authorLoader != nil {

		response.Meta["author"] = nil
		response.Meta["editor"] = nil

		ids := authorIDs{
			creatorID: extractIntField(result, r.authorColumn),
			editorID:  extractIntField(result, r.editorColumn),
		}

		if ids.creatorID > 0 || ids.editorID > 0 {

			pendingIDsSlice := make([]int, 0)
			if ids.creatorID > 0 {
				pendingIDsSlice = append(pendingIDsSlice, ids.creatorID)
			}
			if ids.editorID > 0 {
				pendingIDsSlice = append(pendingIDsSlice, ids.editorID)
			}

			var authors []any
			var authorsErr error

			authors, authorsErr = r.authorLoader(r.db, pendingIDsSlice)
			if authorsErr != nil {
				return response, authorsErr
			}

			for _, a := range authors {
				if author, ok := a.(interface{ GetID() int }); ok {
					if author.GetID() == ids.creatorID {
						response.Meta["author"] = a
					}
					if author.GetID() == ids.editorID {
						response.Meta["editor"] = a
					}
				}
			}
		}
	}

	// Get counts
	for _, count := range r.counts {
		var total int64

		countQuery := r.db.Session(&gorm.Session{NewDB: true}).
			Table(count.table).
			Select("COUNT(*)").
			Where(fmt.Sprintf("`%s` = ?", count.foreignKey), id)

		if count.clause != "" {
			countQuery = countQuery.Where(count.clause)
		}

		err := countQuery.Count(&total).Error
		if err != nil {
			return response, err
		}

		countKey := count.table + "_count"
		response.Meta[countKey] = total
	}

	response.Data = result

	return response, nil
}

func extractIntField(v any, fieldName string) int {
	rv := reflect.ValueOf(v)
	fv := rv.FieldByName(fieldName)
	if !fv.IsValid() {
		return 0
	}
	switch fv.Kind() {
	case reflect.Int:
		return int(fv.Int())
	case reflect.Ptr:
		if fv.IsNil() {
			return 0
		}
		return int(fv.Elem().Int())
	case reflect.Struct:
		if ni, ok := fv.Interface().(types.NullInt); ok {
			if p := ni.Get(); p != nil {
				return *p
			}
		}
	}
	return 0
}
