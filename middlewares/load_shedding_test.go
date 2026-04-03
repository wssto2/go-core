package middlewares

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLoadShedding_RejectsWhenOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(LoadShedding(2, http.StatusServiceUnavailable))

	started := atomic.Int32{}
	done := make(chan struct{})
	r.GET("/test", func(c *gin.Context) {
		started.Add(1)
		<-done
		c.String(http.StatusOK, "ok")
	})

	recs := make([]*httptest.ResponseRecorder, 3)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		recs[i] = httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		wg.Add(1)
		go func(rec *httptest.ResponseRecorder, req *http.Request) {
			defer wg.Done()
			r.ServeHTTP(rec, req)
		}(recs[i], req)
	}

	require.Eventually(t, func() bool { return started.Load() == 2 }, 2*time.Second, 10*time.Millisecond)

	// give middleware time to reject third
	time.Sleep(20 * time.Millisecond)
	close(done)

	// wait for all goroutines to finish before reading recorder state
	wg.Wait()

	okCount := 0
	svcCount := 0
	for _, rec := range recs {
		switch rec.Code {
		case http.StatusOK:
			okCount++
		case http.StatusServiceUnavailable:
			svcCount++
		}
	}
	require.Equal(t, 2, okCount)
	require.Equal(t, 1, svcCount)
}

func TestLoadShedding_Returns429WhenConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(LoadShedding(1, http.StatusTooManyRequests)) // 429

	started := atomic.Int32{}
	done := make(chan struct{})
	r.GET("/t2", func(c *gin.Context) {
		started.Add(1)
		<-done
		c.String(http.StatusOK, "ok")
	})

	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/t2", nil)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.ServeHTTP(rec1, req1)
	}()

	// give the first request time to start
	require.Eventually(t, func() bool { return started.Load() == 1 }, time.Second, 10*time.Millisecond)

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/t2", nil)
	r.ServeHTTP(rec2, req2) // synchronous call for second request; should be rejected

	close(done)
	// wait for the first goroutine to finish before reading rec1.Code
	wg.Wait()

	require.Equal(t, http.StatusOK, rec1.Code)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
}
