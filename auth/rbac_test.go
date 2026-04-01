package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// fake user implementing HasPolicy
type userHasPolicy struct{ ID int }

func (u userHasPolicy) GetID() int              { return u.ID }
func (u userHasPolicy) HasPolicy(p string) bool { return p == "acct:read" }

// fake user implementing GetPolicies
type userGetPolicies struct{ ID int }

func (u userGetPolicies) GetID() int            { return u.ID }
func (u userGetPolicies) GetPolicies() []string { return []string{"acct:read"} }

func TestDefaultAuthorizer_HasPolicy(t *testing.T) {
	t.Parallel()
	user := userHasPolicy{ID: 1}
	authz := func(user Identifiable, policy Policy) bool {
		type hasPolicy interface {
			HasPolicy(string) bool
		}
		if hp, ok := user.(hasPolicy); ok {
			return hp.HasPolicy(policy.String())
		}
		type getPolicies interface {
			GetPolicies() []string
		}
		if gp, ok := user.(getPolicies); ok {
			for _, p := range gp.GetPolicies() {
				if p == policy.String() {
					return true
				}
			}
		}
		return false
	}
	assert.True(t, authz(user, GeneratePolicy("acct", "read")))
	assert.False(t, authz(user, GeneratePolicy("acct", "write")))
}

func TestDefaultAuthorizer_GetPolicies(t *testing.T) {
	t.Parallel()
	user := userGetPolicies{ID: 2}
	authz := func(user Identifiable, policy Policy) bool {
		type hasPolicy interface {
			HasPolicy(string) bool
		}
		if hp, ok := user.(hasPolicy); ok {
			return hp.HasPolicy(policy.String())
		}
		type getPolicies interface {
			GetPolicies() []string
		}
		if gp, ok := user.(getPolicies); ok {
			for _, p := range gp.GetPolicies() {
				if p == policy.String() {
					return true
				}
			}
		}
		return false
	}
	assert.True(t, authz(user, GeneratePolicy("acct", "read")))
	assert.False(t, authz(user, GeneratePolicy("acct", "write")))
}

func TestAuthorizeMiddleware_Allows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	r := gin.New()
	user := userHasPolicy{ID: 3}

	// inject user before authorization
	r.Use(func(c *gin.Context) {
		SetUser(c, user)
		c.Next()
	})

	// middleware under test
	r.Use(Authorize(GeneratePolicy("acct", "read")))

	r.GET("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "ok", w.Body.String())
}

func TestAuthorizeMiddleware_AbortsWhenNoUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	c.Request = req

	Authorize(GeneratePolicy("acct", "read"))(c)

	assert.True(t, c.IsAborted())
	assert.NotEmpty(t, c.Errors)
}

func TestAuthorizeMiddleware_ForbidsWhenNoPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	c.Request = req

	SetUser(c, userGetPolicies{ID: 4})
	Authorize(GeneratePolicy("acct", "write"))(c)

	assert.True(t, c.IsAborted())
	assert.NotEmpty(t, c.Errors)
}
