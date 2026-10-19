//nolint:testpackage,paralleltest // uses the package's runnable helper
package gocore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/route"
)

type noteInput struct {
	Text string `json:"text" validation:"max:100000000"`
}

var (
	noteRoute = route.Post[noteInput, string]("/note").Name("limit.note").Public()
	bigRoute  = route.Post[noteInput, string]("/big").Name("limit.big").Public().MaxBody(64 << 10)
	rawRoute  = route.Raw(http.MethodPost, "/raw").Name("limit.raw").Public()
)

func limited(t *testing.T, limit int64) http.Handler {
	t.Helper()

	app, _ := runnable(t)
	app.cfg.HTTP.MaxBodyBytes = limit
	app.Routes(
		noteRoute.To(func(_ context.Context, _ noteInput) (string, error) { return "ok", nil }),
		bigRoute.To(func(_ context.Context, _ noteInput) (string, error) { return "ok", nil }),
		rawRoute.To(func(ctx *gin.Context) {
			if _, err := io.ReadAll(ctx.Request.Body); err != nil {
				_ = ctx.Error(err)
				return
			}

			ctx.String(http.StatusOK, "ok")
		}),
	)

	h, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}

	return h
}

func post(h http.Handler, path, contentType string, size int, chunked bool) *httptest.ResponseRecorder {
	body := `{"text":"` + strings.Repeat("a", size) + `"}`
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	if chunked {
		req.ContentLength = -1
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestJSONBodyOverTheLimitAnswers413(t *testing.T) {
	h := limited(t, 1000)

	for name, tc := range map[string]struct {
		path    string
		size    int
		chunked bool
		want    int
	}{
		"under the limit":          {"/note", 500, false, 200},
		"over, length known":       {"/note", 2000, false, 413},
		"over, length unknown":     {"/note", 2000, true, 413},
		"raw handler passes error": {"/raw", 2000, true, 413},
		"route override raises it": {"/big", 2000, false, 200},
		"override still limits":    {"/big", 70 << 10, false, 413},
	} {
		if got := post(h, tc.path, "application/json", tc.size, tc.chunked).Code; got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
}

func TestMultipartIsLeftToTheUpload(t *testing.T) {
	h := limited(t, 1000)

	if got := post(h, "/raw", "multipart/form-data; boundary=x", 2000, false).Code; got != http.StatusOK {
		t.Errorf("multipart over the JSON limit: %d", got)
	}
}

func TestDefaultJSONLimitIs4MiB(t *testing.T) {
	h := limited(t, 0)

	if got := post(h, "/note", "application/json", 3<<20, false).Code; got != http.StatusOK {
		t.Errorf("3 MiB: %d", got)
	}

	if got := post(h, "/note", "application/json", 4<<20+1, false).Code; got != http.StatusRequestEntityTooLarge {
		t.Errorf("over 4 MiB: %d", got)
	}
}

func TestNegativeLimitTurnsItOff(t *testing.T) {
	h := limited(t, -1)

	if got := post(h, "/note", "application/json", 5<<20, false).Code; got != http.StatusOK {
		t.Errorf("5 MiB with no limit: %d", got)
	}
}
