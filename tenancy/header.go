package tenancy

import (
	"net/http"
	"strconv"
	"strings"
)

// FromHeader returns an http middleware that parses an integer tenant id from
// the given header and stores it using WithTenantID. If parsing fails or header
// missing, the request proceeds without a tenant set.
func FromHeader(headerName string) func(next http.Handler) http.Handler {
	if headerName == "" {
		headerName = "X-Tenant-ID"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			val := strings.TrimSpace(r.Header.Get(headerName))
			if val == "" {
				next.ServeHTTP(w, r)
				return
			}
			id, err := strconv.Atoi(val)
			if err != nil || id <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			ctx := WithTenantID(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
