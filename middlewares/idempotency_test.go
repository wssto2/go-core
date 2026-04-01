package middlewares

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestIdempotency_NoHeader_AllowsDuplicates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewInMemoryIdempotencyStore(0)
	r := gin.New()
	r.Use(Idempotency(store))

	var calls int32
	r.POST("/do", func(c *gin.Context) {
		atomic.AddInt32(&calls, 1)
		c.String(http.StatusOK, "ok")
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("POST", "/do", strings.NewReader(""))
	r.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/do", strings.NewReader(""))
	r.ServeHTTP(w2, req2)

	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
	assert.Equal(t, "ok", strings.TrimSpace(w1.Body.String()))
	assert.Equal(t, "ok", strings.TrimSpace(w2.Body.String()))
}

func TestIdempotency_DeduplicatesConcurrentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewInMemoryIdempotencyStore(0)
	r := gin.New()
	r.Use(Idempotency(store))

	var calls int32
	r.POST("/do", func(c *gin.Context) {
		atomic.AddInt32(&calls, 1)
		// simulate work
		time.Sleep(80 * time.Millisecond)
		c.String(http.StatusOK, "done")
	})

	key := "abc-123"
	workers := 2
	wg := sync.WaitGroup{}
	wg.Add(workers)

	results := make([]*httptest.ResponseRecorder, workers)

	for i := 0; i < workers; i++ {
		results[i] = httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/do", strings.NewReader(""))
		req.Header.Set(HeaderIdempotencyKey, key)
		go func(rec *httptest.ResponseRecorder, req *http.Request) {
			defer wg.Done()
			r.ServeHTTP(rec, req)
		}(results[i], req)
	}

	wg.Wait()

	// only one actual handler invocation should have occurred
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
	for i := 0; i < workers; i++ {
		assert.Equal(t, "done", strings.TrimSpace(results[i].Body.String()))
		assert.Equal(t, http.StatusOK, results[i].Code)
	}
}

func TestIdempotency_CachedResponseUsed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewInMemoryIdempotencyStore(0)
	r := gin.New()
	r.Use(Idempotency(store))

	var calls int32
	r.POST("/do", func(c *gin.Context) {
		atomic.AddInt32(&calls, 1)
		c.String(http.StatusCreated, "created")
	})

	key := "cache-1"
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("POST", "/do", strings.NewReader(""))
	req1.Header.Set(HeaderIdempotencyKey, key)
	r.ServeHTTP(w1, req1)

	// second request should get cached response and not call handler
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/do", strings.NewReader(""))
	req2.Header.Set(HeaderIdempotencyKey, key)
	r.ServeHTTP(w2, req2)

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
	assert.Equal(t, "created", strings.TrimSpace(w2.Body.String()))
	assert.Equal(t, http.StatusCreated, w2.Code)
}
