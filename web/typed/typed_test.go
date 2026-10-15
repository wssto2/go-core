package typed_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/pagination"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/web"
	"github.com/wssto2/go-core/web/typed"
)

type row struct {
	Name string `json:"name"`
}

func engine(t *testing.T, mount func(*gin.Engine) error) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	e := gin.New()
	e.Use(middlewares.ErrorHandler(nil, nil, false))
	require.NoError(t, mount(e))

	return e
}

func get(h http.Handler, target string, set func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if set != nil {
		set(req)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

// samePage is a result with every part of the envelope set.
func samePage() *pagination.PageResult[row] {
	return &pagination.PageResult[row]{
		Items: []row{{"a"}, {"b"}}, Meta: map[string]any{"by": "x"}, Views: []pagination.ViewCount{{Key: "all", Count: 12}},
		Total: 12, PerPage: 2, Page: 3, LastPage: 6, From: 5, To: 6,
	}
}

func TestToPageAnswersWhatWritePageWrites(t *testing.T) {
	old := engine(t, func(e *gin.Engine) error {
		e.GET("/x", func(c *gin.Context) { web.WritePage(c, samePage()) })

		return nil
	})

	r := route.Get[route.None, route.Page[row]]("/x").Public().To(func(context.Context, route.None) (route.Page[row], error) {
		return typed.ToPage(samePage()), nil
	})
	typedEngine := engine(t, func(e *gin.Engine) error { return r.Mount(e, route.Security{}) })

	want, got := get(old, "/x", nil), get(typedEngine, "/x", nil)

	assert.Equal(t, http.StatusOK, got.Code)
	assert.Equal(t, want.Body.String(), got.Body.String())
}

func TestToPageOfAResultWithoutMetaOrViews(t *testing.T) {
	page := typed.ToPage(&pagination.PageResult[row]{Items: []row{}, Page: 1})

	assert.Equal(t, map[string]any{}, page.Result.Meta, "a nil meta becomes an empty one the page merges into")
	assert.Equal(t, []row{}, page.Result.Data)
}

func TestToPageDoesNotWriteIntoTheResultsMeta(t *testing.T) {
	result := samePage()
	typed.ToPage(result)

	assert.Equal(t, map[string]any{"by": "x"}, result.Meta)
}

func TestRecordAnswersWhatWriteResourceWrites(t *testing.T) {
	meta := map[string]any{"author": "ann"}
	old := engine(t, func(e *gin.Engine) error {
		e.GET("/x", func(c *gin.Context) {
			web.WriteResource(c, pagination.Resource[row]{Data: row{"a"}, Meta: meta}, nil)
		})

		return nil
	})

	r := route.Get[route.None, route.WithMeta[row, typed.RecordMeta]]("/x").Public().To(
		func(context.Context, route.None) (route.WithMeta[row, typed.RecordMeta], error) {
			return typed.Record(row{"a"}, meta), nil
		})
	typedEngine := engine(t, func(e *gin.Engine) error { return r.Mount(e, route.Security{}) })

	want, got := get(old, "/x", nil), get(typedEngine, "/x", nil)

	assert.Equal(t, http.StatusOK, got.Code)
	assert.JSONEq(t, `{"success":true,"data":{"name":"a"},"meta":{"author":"ann"}}`, got.Body.String())
	assert.JSONEq(t, want.Body.String(), got.Body.String())
}

func TestAccessToken(t *testing.T) {
	var seen string

	r := route.Get[route.None, route.Empty]("/x").Public().To(func(ctx context.Context, _ route.None) (route.Empty, error) {
		seen = typed.AccessToken(ctx)

		return route.Empty{}, nil
	})
	h := engine(t, func(e *gin.Engine) error { return r.Mount(e, route.Security{}) })

	cases := map[string]struct {
		set  func(*http.Request)
		want string
	}{
		"cookie": {func(req *http.Request) { req.AddCookie(&http.Cookie{Name: "access_token", Value: "from-cookie"}) }, "from-cookie"},
		"bearer": {func(req *http.Request) { req.Header.Set("Authorization", "Bearer  from-header ") }, "from-header"},
		"cookie wins": {func(req *http.Request) {
			req.AddCookie(&http.Cookie{Name: "access_token", Value: "from-cookie"})
			req.Header.Set("Authorization", "Bearer from-header")
		}, "from-cookie"},
		"other scheme": {func(req *http.Request) { req.Header.Set("Authorization", "Basic abc") }, ""},
		"none":         {nil, ""},
	}

	for name, c := range cases {
		seen = "unset"

		get(h, "/x", c.set)

		assert.Equal(t, c.want, seen, name)
	}

	assert.Empty(t, typed.AccessToken(context.Background()), "outside a route there is no exchange")
}
