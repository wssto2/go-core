package tenancy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFromHeader_InsertsTenant(t *testing.T) {
	mw := FromHeader("X-Tenant-ID")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := TenantIDFromContext(r.Context())
		if !ok {
			t.Fatalf("tenant id missing")
		}
		if id != 123 {
			t.Fatalf("unexpected tenant id: %d", id)
		}
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Tenant-ID", "123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
}
