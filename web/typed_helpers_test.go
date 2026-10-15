package web_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/pagination"
	"github.com/wssto2/go-core/web"
)

var errBoom = errors.New("boom")

// serve runs handler on GET /x/:id behind the standard error handler and returns the response.
func serve(t *testing.T, target string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middlewares.ErrorHandler(slog.New(slog.DiscardHandler), nil, true))
	router.GET("/x/:id", handler)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil))

	return rec
}

func TestMapped(t *testing.T) {
	t.Parallel()

	got, err := web.Mapped(strconv.Itoa)(7, nil)
	require.NoError(t, err)
	assert.Equal(t, "7", got)

	got, err = web.Mapped(strconv.Itoa)(7, errBoom)
	require.ErrorIs(t, err, errBoom)
	assert.Empty(t, got, "a failed call answers the zero value")
}

func TestValue(t *testing.T) {
	t.Parallel()

	record := 5
	got, err := web.Value(&record)
	require.NoError(t, err)
	assert.Equal(t, 5, got)

	got, err = web.Value[int](nil)
	require.Error(t, err)
	assert.Zero(t, got)

	var app *apperr.AppError
	require.ErrorAs(t, err, &app)
	assert.Equal(t, apperr.CodeInternal, app.Code, "no record is the service's mistake")
}

func TestPositiveID(t *testing.T) {
	t.Parallel()

	require.NoError(t, web.PositiveID("id", 1))

	for _, id := range []int{0, -4} {
		err := web.PositiveID("evaluationID", id)

		var app *apperr.AppError
		require.ErrorAs(t, err, &app)
		assert.Equal(t, apperr.CodeBadRequest, app.Code)
		assert.Equal(t, "invalid evaluationID: must be a positive integer", app.Message)
	}
}

func TestPositivePathIntAnswersWhatPathIntAnswers(t *testing.T) {
	t.Parallel()

	require.NoError(t, web.PositivePathInt("id", 3))

	viaPathInt := serve(t, "/x/0", func(ctx *gin.Context) { web.PathInt(ctx, "id") })
	viaPositive := serve(t, "/x/0", func(ctx *gin.Context) {
		_ = ctx.Error(web.PositivePathInt("id", 0))
		ctx.Abort()
	})

	assert.Equal(t, http.StatusUnprocessableEntity, viaPositive.Code)
	assert.Equal(t, viaPathInt.Body.String(), viaPositive.Body.String(), "one wire for both helpers")
	assert.Contains(t, viaPositive.Body.String(), `"id":[`)
}

type user int

func (u user) GetID() int { return int(u) }

func TestSignedIn(t *testing.T) {
	t.Parallel()

	_, err := web.SignedIn(context.Background())

	var app *apperr.AppError
	require.ErrorAs(t, err, &app)
	assert.Equal(t, apperr.CodeUnauthenticated, app.Code)
	assert.Equal(t, "not authenticated", app.Message)

	rec := serve(t, "/x/1", func(ctx *gin.Context) {
		auth.SetUser(ctx, user(9))

		who, err := web.SignedIn(ctx.Request.Context())
		require.NoError(t, err)
		ctx.String(http.StatusOK, strconv.Itoa(who.GetID()))
	})
	assert.Equal(t, "9", rec.Body.String())
}

func TestPageFromValues(t *testing.T) {
	t.Parallel()

	got := web.PageFromValues(url.Values{
		"page": {"3"}, "length": {"25"}, "search": {"golf"}, "order_column": {"name"}, "order_direction": {"desc"},
		"view": {"open"}, "status": {"new", "ignored"}, "dealer": {""},
	})

	assert.Equal(t, pagination.Page{
		Number: 3, Size: 25, Search: "golf", OrderCol: "name", OrderDir: "desc", View: "open",
		Filters: map[string]string{"status": "new", "dealer": ""},
	}, got)

	got = web.PageFromValues(url.Values{"per_page": {"10"}, "length": {"99"}, "order_col": {"id"}, "order_column": {"x"}})
	assert.Equal(t, 1, got.Number, "page defaults to 1")
	assert.Equal(t, 10, got.Size, "per_page wins over its legacy alias")
	assert.Equal(t, "id", got.OrderCol)
	assert.Empty(t, got.Filters, "reserved keys are not filters")

	assert.Equal(t, pagination.Page{
		Number: 1, Size: 0, Search: "", OrderCol: "", OrderDir: "", View: "", Filters: map[string]string{},
	}, web.PageFromValues(nil))
}

func TestWritePage(t *testing.T) {
	t.Parallel()

	rec := serve(t, "/x/1", func(ctx *gin.Context) {
		web.WritePage(ctx, &pagination.PageResult[string]{
			Items: []string{"a"}, Meta: map[string]any{"by": "x"}, Views: []pagination.ViewCount{{Key: "all", Count: 4}},
			Total: 4, PerPage: 1, Page: 2, LastPage: 4, From: 2, To: 2,
		})
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":["a"],"meta":{"total":4,"page":2,"per_page":1,"last_page":4,"from":2,"to":2,`+
		`"by":"x","views":[{"key":"all","count":4}]}}`, rec.Body.String())
}

func TestWritePageOfNoRowsKeepsDataKey(t *testing.T) {
	t.Parallel()

	rec := serve(t, "/x/1", func(ctx *gin.Context) {
		web.WritePage(ctx, &pagination.PageResult[string]{
			Items: []string{}, Meta: nil, Views: nil, Total: 0, PerPage: 0, Page: 0, LastPage: 0, From: 0, To: 0,
		})
	})

	assert.Contains(t, rec.Body.String(), `"data":[]`)
}

func TestWriteResource(t *testing.T) {
	t.Parallel()

	rec := serve(t, "/x/1", func(ctx *gin.Context) {
		web.WriteResource(ctx, pagination.Resource[string]{Data: "row", Meta: map[string]any{"author": "ann"}}, nil)
	})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":"row","meta":{"author":"ann"}}`, rec.Body.String())

	rec = serve(t, "/x/1", func(ctx *gin.Context) {
		web.WriteResource(ctx, pagination.Resource[string]{Data: "", Meta: nil}, apperr.NotFound("gone"))
	})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWriteAccepted(t *testing.T) {
	t.Parallel()

	rec := serve(t, "/x/1", func(ctx *gin.Context) { web.WriteAccepted(ctx, map[string]string{"job": "q1"}) })

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":{"job":"q1"}}`, rec.Body.String())
}
