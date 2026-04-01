package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/wssto2/go-core/datatable"
)

func TestJSONResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	c.Request = req

	JSON(c, http.StatusOK, map[string]string{"msg": "hello"})

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Success bool              `json:"success"`
		Data    map[string]string `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "hello", resp.Data["msg"])
}

func TestCreatedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/", nil)
	c.Request = req

	Created(c, map[string]any{"id": 1})

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.True(t, resp.Success)
	if val, ok := resp.Data["id"]; ok {
		// JSON numbers may decode to float64 when using interface{} values
		assert.Equal(t, float64(1), val)
	} else {
		t.Fatalf("expected id in response data")
	}
}

func TestNoContentResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("DELETE", "/", nil)
	c.Request = req

	NoContent(c)
	// ensure headers are flushed so recorder observes the status code
	c.Writer.WriteHeaderNow()

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 0, w.Body.Len())
}

func TestPaginatedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	c.Request = req

	result := &datatable.DatatableResult[int]{
		Data:     []int{1, 2},
		Total:    2,
		PerPage:  10,
		Page:     1,
		LastPage: 1,
		From:     1,
		To:       2,
	}

	Paginated[int](c, result)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Success bool `json:"success"`
		Data    []int
		Meta    struct {
			Total    int64 `json:"total"`
			Page     int   `json:"page"`
			PerPage  int   `json:"per_page"`
			LastPage int   `json:"last_page"`
			From     int   `json:"from"`
			To       int   `json:"to"`
		} `json:"meta"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, int64(2), resp.Meta.Total)
	assert.Equal(t, 1, resp.Meta.Page)
	assert.Equal(t, 10, resp.Meta.PerPage)
	assert.Equal(t, 1, resp.Meta.LastPage)
	assert.Equal(t, 1, resp.Meta.From)
	assert.Equal(t, 2, resp.Meta.To)
	assert.Equal(t, []int{1, 2}, resp.Data)
}
