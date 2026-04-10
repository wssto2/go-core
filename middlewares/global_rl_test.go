package middlewares_test

import (
"net/http"
"net/http/httptest"
"testing"
"time"

"github.com/gin-gonic/gin"
"github.com/wssto2/go-core/middlewares"
"github.com/wssto2/go-core/ratelimit"
)

func TestRateLimit_GlobalKey_SharedAcrossUsers(t *testing.T) {
gin.SetMode(gin.TestMode)
r := gin.New()
// global limit of 3 — perUser=false, perEndpoint=false → key="global"
r.Use(middlewares.RateLimit(ratelimit.NewInMemoryLimiter(3, time.Minute), false, false))
r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

ok, limited := 0, 0
// simulate 5 requests from two different IPs
ips := []string{"1.2.3.4", "5.6.7.8", "9.10.11.12", "13.14.15.16", "17.18.19.20"}
for _, ip := range ips {
req, _ := http.NewRequest(http.MethodGet, "/test", nil)
req.Header.Set("X-Forwarded-For", ip)
w := httptest.NewRecorder()
r.ServeHTTP(w, req)
if w.Code == http.StatusTooManyRequests {
limited++
} else {
ok++
}
}
if ok != 3 || limited != 2 {
t.Errorf("expected 3 ok + 2 limited, got %d ok + %d limited", ok, limited)
}
}
