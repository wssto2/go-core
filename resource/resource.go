package resource

import (
	"fmt"
	"reflect"

	"github.com/wssto2/go-core/database/types"
	"gorm.io/gorm"
)

type AuthorLoader func(db *gorm.DB, ids []int) ([]any, error)

type Count struct {
	TableName  string `json:"table_name"`
	Count      int64  `json:"count"`
	ForeignKey string `json:"foreign_key"`
	Clause     string `json:"clause"`
}

type Resource[T any] struct {
	db                   *gorm.DB
	shouldIncludeAuthors bool
	counts               []Count
	tableName            string
	authorLoader         AuthorLoader
	authorColumn         string
	editorColumn         string
	err                  error
}

type Response[T any] struct {
	Data T              `json:"data"`
	Meta map[string]any `json:"meta"`
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
	r.shouldIncludeAuthors = true
	r.authorColumn = authorField
	r.editorColumn = editorField
	r.authorLoader = loader
	return r
}

// WithQuery.
func (r *Resource[T]) WithQuery(callback func(query *gorm.DB, tableName string) *gorm.DB) *Resource[T] {
	r.db = callback(r.db.Session(&gorm.Session{NewDB: true}), r.tableName)

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
		TableName:  tableName,
		ForeignKey: foreignKey,
		Clause:     clause,
	})

	return r
}

// WithoutDeleted filters soft-deleted records using the given column name.
// For standard GORM soft-delete (deleted_at IS NULL), pass "deleted_at".
func (r *Resource[T]) WithoutDeleted(column string) *Resource[T] {
	r.db = r.db.Where(fmt.Sprintf("%s.%s IS NULL", r.tableName, column))
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
	if r.shouldIncludeAuthors {
		createdByOriginal := reflect.ValueOf(result).FieldByName(r.authorColumn)
		updatedByOriginal := reflect.ValueOf(result).FieldByName(r.editorColumn)

		response.Meta["author"] = nil
		response.Meta["editor"] = nil

		pendingIDs := make(map[string]int, 0)

		if createdByOriginal.IsValid() {
			// CreatedBy can be int, database.NullInt or pointer to int
			var createdByID int
			switch createdByOriginal.Kind() {
			case reflect.Int:
				createdByID = createdByOriginal.Interface().(int)
			case reflect.Ptr:
				createdByID = createdByOriginal.Elem().Interface().(int)
			case reflect.Struct:
				switch t := createdByOriginal.Interface().(type) {
				case types.NullInt:
					if ptr := t.Get(); ptr != nil {
						createdByID = *ptr
					}
				}
			}

			if createdByID > 0 {
				pendingIDs["author"] = createdByID
			}
		}

		if updatedByOriginal.IsValid() {
			// UpdatedBy can be int, database.NullInt or pointer to int
			var updatedByID int
			switch updatedByOriginal.Kind() {
			case reflect.Int:
				updatedByID = updatedByOriginal.Interface().(int)
			case reflect.Ptr:
				updatedByID = updatedByOriginal.Elem().Interface().(int)
			case reflect.Struct:
				switch t := updatedByOriginal.Interface().(type) {
				case types.NullInt:
					if ptr := t.Get(); ptr != nil {
						updatedByID = *ptr
					}
				}
			}

			if updatedByID > 0 {
				pendingIDs["editor"] = updatedByID
			}
		}

		if len(pendingIDs) > 0 {
			var authors []any

			pendingIDsSlice := make([]int, 0)
			for _, pendingID := range pendingIDs {
				pendingIDsSlice = append(pendingIDsSlice, pendingID)
			}

			authors, err := r.authorLoader(r.db, pendingIDsSlice)
			if err != nil {
				return response, err
			}

			for _, a := range authors {
				if author, ok := a.(interface{ GetID() int }); ok {
					if author.GetID() == pendingIDs["author"] {
						response.Meta["author"] = a
					}
					if author.GetID() == pendingIDs["editor"] {
						response.Meta["editor"] = a
					}
				}
			}
		}
	}

	// Get counts
	for _, count := range r.counts {
		var total int64

		countQuery := r.db.Table(count.TableName).
			Select("COUNT(*)").
			Where(fmt.Sprintf("`%s` = ?", count.ForeignKey), id)

		if count.Clause != "" {
			countQuery = countQuery.Where(count.Clause)
		}

		err := countQuery.Count(&total).Error
		if err != nil {
			return response, err
		}

		countKey := count.TableName + "_count"
		response.Meta[countKey] = total
	}

	response.Data = result

	return response, nil
}
