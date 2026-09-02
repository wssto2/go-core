package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
)

func newParamCtx(key, value string) *gin.Context {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	ctx.Params = gin.Params{{Key: key, Value: value}}

	return ctx
}

func TestGetPathIDNamed_Valid(t *testing.T) {
	t.Parallel()

	ctx := newParamCtx("customerID", "42")

	id, ok := GetPathIDNamed(ctx, "customerID")

	if !ok || id != 42 {
		t.Fatalf("want (42, true), got (%d, %v)", id, ok)
	}

	if ctx.IsAborted() {
		t.Fatal("valid id must not abort")
	}
}

func TestGetPathIDNamed_RejectsNonNumeric(t *testing.T) {
	t.Parallel()

	ctx := newParamCtx("customerID", "not-a-number")

	id, ok := GetPathIDNamed(ctx, "customerID")

	if ok || id != 0 {
		t.Fatalf("want (0, false), got (%d, %v)", id, ok)
	}

	if !ctx.IsAborted() {
		t.Fatal("malformed id must abort")
	}

	if status := apperr.GetHTTPStatus(ctx.Errors.Last().Err); status != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", status)
	}
}

func TestGetPathIDNamed_RejectsNonPositive(t *testing.T) {
	t.Parallel()

	for _, val := range []string{"0", "-1"} {
		ctx := newParamCtx("evaluationID", val)

		_, ok := GetPathIDNamed(ctx, "evaluationID")

		if ok {
			t.Fatalf("value %q: want ok=false", val)
		}

		if !ctx.IsAborted() {
			t.Fatalf("value %q: must abort", val)
		}
	}
}

func TestGetPathIDNamed_RejectsFloat(t *testing.T) {
	t.Parallel()

	ctx := newParamCtx("id", "1.5")

	if _, ok := GetPathIDNamed(ctx, "id"); ok {
		t.Fatal("float value must be rejected")
	}
}

func TestGetPathID_DelegatesToNamed(t *testing.T) {
	t.Parallel()

	ctx := newParamCtx("id", "7")

	id, ok := GetPathID(ctx)

	if !ok || id != 7 {
		t.Fatalf("want (7, true), got (%d, %v)", id, ok)
	}
}
