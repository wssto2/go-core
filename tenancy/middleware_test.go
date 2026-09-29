package tenancy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/tenancy"
)

type tenantUser struct {
	id, tenant int
	has        bool
}

func (u tenantUser) GetID() int       { return u.id }
func (u tenantUser) GetTenantID() int { return u.tenant }
func (u tenantUser) HasTenant() bool  { return u.has }

type plainUser struct{}

func (plainUser) GetID() int { return 1 }

func TestFromAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		user       auth.Identifiable // nil: not authenticated
		wantTenant int
		wantAll    bool
	}{
		{"tenant user pins its tenant", tenantUser{id: 1, tenant: 7, has: true}, 7, false},
		{"user without a tenant is an explicit super-admin", tenantUser{id: 2}, 0, true},
		{"user that is not TenantAware pins nothing", plainUser{}, 0, false},
		{"no user pins nothing", nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(ctx *gin.Context) {
				if tt.user != nil {
					auth.SetUser(ctx, tt.user)
				}
			})
			r.Use(tenancy.FromAuthenticatedUser())
			var gotTenant int
			var gotAll bool
			r.GET("/", func(ctx *gin.Context) {
				gotTenant, _ = tenancy.TenantIDFromContext(ctx.Request.Context())
				gotAll = tenancy.AllTenantsFromContext(ctx.Request.Context())
				ctx.Status(http.StatusOK)
			})
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, tt.wantTenant, gotTenant)
			assert.Equal(t, tt.wantAll, gotAll)
		})
	}
}
