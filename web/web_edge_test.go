package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
)

// --- Handle / HandleCreate ---

func TestHandle_ErrorCallsFail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	Handle[any](c, nil, apperr.NotFound("missing"))

	if !c.IsAborted() {
		t.Fatal("expected context to be aborted after Fail")
	}
	if len(c.Errors) == 0 {
		t.Fatal("expected error to be attached to context")
	}
}

func TestHandle_SuccessWritesJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	Handle(c, map[string]string{"key": "val"}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Success {
		t.Fatal("expected success=true")
	}
}

func TestHandleCreate_ErrorCallsFail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	HandleCreate[any](c, nil, apperr.Internal(nil))

	if !c.IsAborted() {
		t.Fatal("expected abort on error")
	}
}

func TestHandleCreate_SuccessWrites201(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	HandleCreate(c, map[string]int{"id": 42}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

// --- GetParamInt / GetQueryInt ---

func TestGetParamInt_MissingReturnsZeroFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	v, ok := GetParamInt(c, "id")
	if ok || v != 0 {
		t.Fatalf("expected (0, false), got (%d, %v)", v, ok)
	}
}

func TestGetQueryInt_ValidValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?page=5", nil)

	v, ok := GetQueryInt(c, "page")
	if !ok || v != 5 {
		t.Fatalf("expected (5, true), got (%d, %v)", v, ok)
	}
}

func TestGetQueryInt_NonIntegerReturnsFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?page=abc", nil)

	v, ok := GetQueryInt(c, "page")
	if ok || v != 0 {
		t.Fatalf("expected (0, false), got (%d, %v)", v, ok)
	}
}

func TestGetQueryInt_MissingReturnsFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	v, ok := GetQueryInt(c, "page")
	if ok || v != 0 {
		t.Fatalf("expected (0, false), got (%d, %v)", v, ok)
	}
}
