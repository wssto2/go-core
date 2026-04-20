package datatable_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/datatable"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type ArticleWithDeleted struct {
	ID        int        `gorm:"primaryKey"`
	Title     string     `gorm:"column:title"`
	Status    int        `gorm:"column:status"`
	DeletedAt *time.Time `gorm:"column:deleted_at"`
}

func edgeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Article{}, &ArticleWithDeleted{}))

	articles := []Article{
		{ID: 10, Title: "Foo", Status: 1},
		{ID: 11, Title: "Bar", Status: 0},
		{ID: 12, Title: "Baz", Status: 1},
	}
	require.NoError(t, db.Create(&articles).Error)

	now := time.Now()
	withDeleted := []ArticleWithDeleted{
		{ID: 1, Title: "Alive", DeletedAt: nil},
		{ID: 2, Title: "Dead", DeletedAt: &now},
	}
	require.NoError(t, db.Create(&withDeleted).Error)
	return db
}

func TestGet_SecondPage_CorrectOffset(t *testing.T) {
	db := edgeTestDB(t)
	params := datatable.QueryParams{Page: 2, PerPage: 2, OrderCol: "id", OrderDir: "asc", Filters: map[string]string{}}

	result, err := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		Get(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(3), result.Total)
	assert.Len(t, result.Data, 1, "page 2 with per_page=2 on 3 rows should return 1 row")
	assert.Equal(t, 3, result.From)
	assert.Equal(t, 3, result.To)
}

func TestGet_InvalidSchema_ReturnsError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	type NotAModel struct{ X int } // no gorm annotations, no table
	params := datatable.DefaultParams()
	_, err = datatable.New[NotAModel](db, params).
		WithColumns([]string{"x"}).
		Get(context.Background())
	assert.Error(t, err, "un-migrated model should fail schema parse")
}

func TestGet_WithDefaultOrder_Applied(t *testing.T) {
	db := edgeTestDB(t)
	params := datatable.QueryParams{Page: 1, PerPage: 10, OrderCol: "", OrderDir: "", Filters: map[string]string{}}

	result, err := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		WithDefaultOrder("id", "desc").
		Get(context.Background())

	require.NoError(t, err)
	require.Len(t, result.Data, 3)
	// descending by id: 12, 11, 10
	assert.Equal(t, 12, result.Data[0].ID)
}

// TestGet_WithDefaultOrder_DoesNotOverrideRequestOrder verifies that
// WithDefaultOrder is a true fallback: user-specified order takes precedence.
func TestGet_WithDefaultOrder_DoesNotOverrideRequestOrder(t *testing.T) {
	db := edgeTestDB(t)
	// User explicitly requested asc order; WithDefaultOrder("id", "desc") must not override it.
	params := datatable.QueryParams{Page: 1, PerPage: 10, OrderCol: "id", OrderDir: "asc", Filters: map[string]string{}}

	result, err := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		WithDefaultOrder("id", "desc").
		Get(context.Background())

	require.NoError(t, err)
	require.Len(t, result.Data, 3)
	// asc by id: 10, 11, 12
	assert.Equal(t, 10, result.Data[0].ID)
}

func TestGet_WithScope_AppliesAdditionalConstraint(t *testing.T) {
	db := edgeTestDB(t)
	params := datatable.QueryParams{Page: 1, PerPage: 10, OrderCol: "id", OrderDir: "asc", Filters: map[string]string{}}

	result, err := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		WithScope(func(q *gorm.DB, tableName string) *gorm.DB {
			return q.Where(tableName+".status = ?", 1)
		}).
		Get(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(2), result.Total)
	for _, a := range result.Data {
		assert.Equal(t, 1, a.Status)
	}
}

func TestGet_WithoutDeleted_ExcludesSoftDeleted(t *testing.T) {
	db := edgeTestDB(t)
	params := datatable.QueryParams{Page: 1, PerPage: 10, OrderCol: "id", OrderDir: "asc", Filters: map[string]string{}}

	result, err := datatable.New[ArticleWithDeleted](db, params).
		WithColumns([]string{"id", "title", "deleted_at"}).
		WithoutDeleted("deleted_at").
		Get(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(1), result.Total)
	assert.Equal(t, "Alive", result.Data[0].Title)
}

func TestGet_WithViews_InactiveViewIgnored(t *testing.T) {
	db := edgeTestDB(t)
	// view key "special" not in request — should return all rows
	params := datatable.QueryParams{Page: 1, PerPage: 10, OrderCol: "id", OrderDir: "asc", View: "", Filters: map[string]string{}}

	result, err := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		WithViews([]datatable.View{
			{
				URIKey: "special",
				Query: func(q *gorm.DB, tbl string) *gorm.DB {
					return q.Where(tbl + ".status = 99") // would return 0 rows if applied
				},
			},
		}).
		Get(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(3), result.Total, "inactive view must not filter results")
}

func TestGet_WithViews_AppendsToExistingViews(t *testing.T) {
	db := edgeTestDB(t)
	params := datatable.QueryParams{Page: 1, PerPage: 10, OrderCol: "id", OrderDir: "asc", View: "active", Filters: map[string]string{}}

	result, err := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		WithView("other", func(q *gorm.DB, tbl string) *gorm.DB {
			return q.Where(tbl + ".status = 99") // should NOT be applied
		}).
		WithViews([]datatable.View{
			{URIKey: "active", Query: func(q *gorm.DB, tbl string) *gorm.DB {
				return q.Where(tbl+".status = ?", 1)
			}},
		}).
		Get(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(2), result.Total, "WithViews must append; 'active' view should filter to status=1")
}

func TestGet_TableName_ReturnsResolvedName(t *testing.T) {
	db := edgeTestDB(t)
	dt := datatable.New[Article](db, datatable.DefaultParams())
	assert.Equal(t, "articles", dt.TableName())
}

func TestDatatableResult_IsEmpty_False(t *testing.T) {
	r := &datatable.DatatableResult[Article]{
		Data: []Article{{ID: 1}},
	}
	assert.False(t, r.IsEmpty())
}
