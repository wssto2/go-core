//nolint:paralleltest,tagalign,noinlineerr,varnamelen // tests
package route_test

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/web"
)

type rowT struct {
	Name string `json:"name"`
}

func pageValue() datatable.DatatableResult[rowT] {
	return datatable.DatatableResult[rowT]{
		Data: []rowT{{"a"}, {"b"}}, Meta: map[string]any{"authors": []int{1}},
		Total: 12, PerPage: 2, Page: 3, LastPage: 6, From: 5, To: 6,
	}
}

func bodyOf(t *testing.T, h http.Handler, method, target string) (int, string) {
	t.Helper()

	rec := do(h, method, target, "")

	return rec.Code, rec.Body.String()
}

func TestPageIsByteIdenticalToWebPaginated(t *testing.T) {
	old := newEngine()
	old.GET("/x", func(c *gin.Context) { v := pageValue(); web.Paginated(c, &v) })

	typed := newEngine()
	r := route.Get[route.None, route.Page[rowT]]("/x").To(func(context.Context, route.None) (route.Page[rowT], error) {
		return route.Page[rowT]{Result: pageValue()}, nil
	})

	if err := r.Mount(typed, open()); err != nil {
		t.Fatal(err)
	}

	c1, b1 := bodyOf(t, old, http.MethodGet, "/x")
	c2, b2 := bodyOf(t, typed, http.MethodGet, "/x")

	if c1 != c2 || b1 != b2 {
		t.Fatalf("web.Paginated %d %s\ntyped %d %s", c1, b1, c2, b2)
	}
}

func TestWithMetaIsByteIdenticalToWebJSONAndTakesAStatus(t *testing.T) {
	type info struct {
		Count int `json:"count"`
	}

	old := newEngine()
	old.POST("/x", func(c *gin.Context) { web.JSON(c, http.StatusCreated, []rowT{{"a"}}, info{3}) })

	typed := newEngine()
	r := route.Post[route.None, route.WithMeta[[]rowT, info]]("/x").Status(http.StatusCreated).
		To(func(context.Context, route.None) (route.WithMeta[[]rowT, info], error) {
			return route.WithMeta[[]rowT, info]{Items: []rowT{{"a"}}, Info: info{3}}, nil
		})

	if err := r.Mount(typed, open()); err != nil {
		t.Fatal(err)
	}

	c1, b1 := bodyOf(t, old, http.MethodPost, "/x")
	c2, b2 := bodyOf(t, typed, http.MethodPost, "/x")

	if c1 != http.StatusCreated || c1 != c2 || b1 != b2 {
		t.Fatalf("web.JSON %d %s\ntyped %d %s", c1, b1, c2, b2)
	}
}

type filterInput struct {
	Page    int        `query:"page"`
	Filters url.Values `json:"-" query:"*"`
}

func TestFreeFormQueryTakesTheUndeclaredParametersAndMatchesParamsFromGin(t *testing.T) {
	const query = "page=2&per_page=5&search=ab&order_col=name&order_dir=desc&view=v&dealer=3&status=a&status=b&length=9"

	var got datatable.QueryParams

	var rest url.Values

	r := route.Get[filterInput, string]("/x").To(func(_ context.Context, in filterInput) (string, error) {
		rest = in.Filters
		got = datatable.ParamsFromValues(in.Filters)

		return "ok", nil
	})
	if !r.Spec().FreeFormQuery {
		t.Fatal("spec must mark the free-form query")
	}

	e := newEngine()
	if err := r.Mount(e, open()); err != nil {
		t.Fatal(err)
	}

	var want datatable.QueryParams

	e.GET("/g", func(c *gin.Context) { want = datatable.ParamsFromGin(c) })

	if code, body := bodyOf(t, e, http.MethodGet, "/x?"+query); code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}

	do(e, http.MethodGet, "/g?"+query, "")

	if _, declared := rest["page"]; declared || rest.Get("dealer") != "3" || len(rest["status"]) != 2 {
		t.Fatalf("free-form values %v", rest)
	}

	// page is declared, so the handler's params only see page through the full query: compare on that.
	full, _ := url.ParseQuery(query)
	if !reflect.DeepEqual(datatable.ParamsFromValues(full), want) {
		t.Fatalf("ParamsFromValues %+v\nParamsFromGin %+v", datatable.ParamsFromValues(full), want)
	}

	if got.Filters["dealer"] != "3" {
		t.Fatalf("filters %v", got.Filters)
	}
}

func TestFreeFormQueryRefusals(t *testing.T) {
	type two struct {
		A url.Values `json:"-" query:"*"`
		B url.Values `json:"-" query:"*"`
	}

	type wrong struct {
		A map[string]string `json:"-" query:"*"`
	}

	r2 := route.Get[two, string]("/x").To(func(context.Context, two) (string, error) { return "", nil })
	if err := r2.Mount(newEngine(), open()); err == nil {
		t.Fatal("two free-form fields must be refused")
	}

	rw := route.Get[wrong, string]("/x").To(func(context.Context, wrong) (string, error) { return "", nil })
	if err := rw.Mount(newEngine(), open()); err == nil {
		t.Fatal("a free-form field that is not url.Values must be refused")
	}
}
