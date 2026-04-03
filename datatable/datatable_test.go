package datatable_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/datatable"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// testDB returns an in-memory SQLite DB seeded with sample rows.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Article{}))
	articles := []Article{
		{ID: 1, Title: "Alpha post", Status: 1},
		{ID: 2, Title: "Beta post", Status: 0},
		{ID: 3, Title: "Gamma article", Status: 1},
		{ID: 4, Title: "Delta article", Status: 0},
		{ID: 5, Title: "Epsilon entry", Status: 1},
	}
	require.NoError(t, db.Create(&articles).Error)
	return db
}

type Article struct {
	ID     int    `gorm:"primaryKey"`
	Title  string `gorm:"column:title"`
	Status int    `gorm:"column:status"`
}

// --- QueryParams helpers ---

func TestQueryParams_GetPage_Defaults(t *testing.T) {
	p := datatable.QueryParams{Page: 0}
	assert.Equal(t, 1, p.GetPage())
}

func TestQueryParams_GetPerPage_Defaults(t *testing.T) {
	p := datatable.QueryParams{PerPage: 0}
	assert.Equal(t, 10, p.GetPerPage())

	p2 := datatable.QueryParams{PerPage: 999}
	assert.Equal(t, 10, p2.GetPerPage())
}

func TestQueryParams_GetPerPage_Valid(t *testing.T) {
	p := datatable.QueryParams{PerPage: 25}
	assert.Equal(t, 25, p.GetPerPage())
}

func TestDefaultParams(t *testing.T) {
	p := datatable.DefaultParams()
	assert.Equal(t, 1, p.GetPage())
	assert.Equal(t, 10, p.GetPerPage())
	assert.Equal(t, "id", p.OrderCol)
	assert.Equal(t, "asc", p.OrderDir)
	assert.NotNil(t, p.Filters)
}

// --- Datatable.Get() ---

func TestGet_ReturnsPaginatedResults(t *testing.T) {
	db := testDB(t)
	params := datatable.QueryParams{Page: 1, PerPage: 2, OrderCol: "id", OrderDir: "asc", Filters: map[string]string{}}

	dt := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"})

	result, err := dt.Get()
	require.NoError(t, err)
	assert.Equal(t, int64(5), result.Total)
	assert.Len(t, result.Data, 2)
	assert.Equal(t, 3, result.LastPage)
	assert.Equal(t, 1, result.From)
	assert.Equal(t, 2, result.To)
}

func TestGet_Page2(t *testing.T) {
	db := testDB(t)
	params := datatable.QueryParams{Page: 2, PerPage: 2, OrderCol: "id", OrderDir: "asc", Filters: map[string]string{}}

	dt := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"})

	result, err := dt.Get()
	require.NoError(t, err)
	assert.Len(t, result.Data, 2)
	assert.Equal(t, 3, result.From)
	assert.Equal(t, 4, result.To)
}

func TestGet_OrderDesc(t *testing.T) {
	db := testDB(t)
	params := datatable.QueryParams{Page: 1, PerPage: 5, OrderCol: "id", OrderDir: "desc", Filters: map[string]string{}}

	dt := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"})

	result, err := dt.Get()
	require.NoError(t, err)
	require.Len(t, result.Data, 5)
	assert.Equal(t, 5, result.Data[0].ID)
	assert.Equal(t, 1, result.Data[4].ID)
}

func TestGet_OrderColNotInWhitelistFallsBackToID(t *testing.T) {
	db := testDB(t)
	params := datatable.QueryParams{
		Page: 1, PerPage: 5,
		OrderCol: "../../malicious", OrderDir: "desc",
		Filters: map[string]string{},
	}

	dt := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"})

	// Should not error — falls back to id column
	result, err := dt.Get()
	require.NoError(t, err)
	assert.Len(t, result.Data, 5)
}

func TestGet_SearchFiltersResults(t *testing.T) {
	db := testDB(t)
	params := datatable.QueryParams{
		Page: 1, PerPage: 10,
		Search: "article", OrderCol: "id", OrderDir: "asc",
		Filters: map[string]string{},
	}

	dt := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		WithSearchableFields([]string{"title"})

	result, err := dt.Get()
	require.NoError(t, err)
	assert.Equal(t, int64(2), result.Total)
	for _, a := range result.Data {
		assert.Contains(t, a.Title, "article")
	}
}

func TestGet_WithStatusFilter(t *testing.T) {
	db := testDB(t)
	params := datatable.QueryParams{
		Page: 1, PerPage: 10, OrderCol: "id", OrderDir: "asc",
		Filters: map[string]string{"status": "active"},
	}

	dt := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		WithStatusFilter("status")

	result, err := dt.Get()
	require.NoError(t, err)
	assert.Equal(t, int64(3), result.Total)
	for _, a := range result.Data {
		assert.Equal(t, 1, a.Status)
	}
}

func TestGet_WithCustomFilter(t *testing.T) {
	db := testDB(t)
	params := datatable.QueryParams{
		Page: 1, PerPage: 10, OrderCol: "id", OrderDir: "asc",
		Filters: map[string]string{"title_prefix": "Alpha"},
	}

	f := datatable.NewFilter("title_prefix", func(q *gorm.DB, val, tbl string) *gorm.DB {
		return q.Where(tbl+".title LIKE ?", val+"%")
	})

	dt := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		WithFilter(f)

	result, err := dt.Get()
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.Total)
	assert.Equal(t, "Alpha post", result.Data[0].Title)
}

func TestGet_WithView(t *testing.T) {
	db := testDB(t)
	params := datatable.QueryParams{
		Page: 1, PerPage: 10, OrderCol: "id", OrderDir: "asc",
		View:    "active_only",
		Filters: map[string]string{},
	}

	dt := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		WithView("active_only", func(q *gorm.DB, tbl string) *gorm.DB {
			return q.Where(tbl+".status = ?", 1)
		})

	result, err := dt.Get()
	require.NoError(t, err)
	assert.Equal(t, int64(3), result.Total)
}

func TestGet_ErrorWhenColumnsNotSet(t *testing.T) {
	db := testDB(t)
	params := datatable.DefaultParams()

	dt := datatable.New[Article](db, params)
	_, err := dt.Get()
	assert.Error(t, err)
}

func TestGet_EmptyResultIsEmpty(t *testing.T) {
	db := testDB(t)
	params := datatable.QueryParams{
		Page: 1, PerPage: 10, OrderCol: "id", OrderDir: "asc",
		Search:  "nonexistent_xyz_123",
		Filters: map[string]string{},
	}

	dt := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		WithSearchableFields([]string{"title"})

	result, err := dt.Get()
	require.NoError(t, err)
	assert.True(t, result.IsEmpty())
	assert.Equal(t, int64(0), result.Total)
	assert.Equal(t, 0, result.From)
	assert.Equal(t, 0, result.To)
}

func TestGet_WithMapper(t *testing.T) {
	db := testDB(t)
	params := datatable.QueryParams{Page: 1, PerPage: 10, OrderCol: "id", OrderDir: "asc", Filters: map[string]string{}}

	dt := datatable.New[Article](db, params).
		WithColumns([]string{"id", "title", "status"}).
		WithMapper(func(a *Article) Article {
			a.Title = "mapped:" + a.Title
			return *a
		})

	result, err := dt.Get()
	require.NoError(t, err)
	for _, a := range result.Data {
		assert.True(t, len(a.Title) > 7, "title should have 'mapped:' prefix")
	}
}
