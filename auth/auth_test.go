package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wssto2/go-core/auth"
)

// --- helpers ---

var testCfg = auth.TokenConfig{
	SecretKey:     "test-secret-key-32-bytes-minimum!",
	Issuer:        "issuer-test",
	TokenDuration: time.Hour,
}

func issueTestToken(t *testing.T, claims auth.Claims) string {
	t.Helper()
	token, err := auth.IssueToken(claims, testCfg)
	require.NoError(t, err)
	return token
}

// --- User ---

// func TestUser_HasPolicy(t *testing.T) {
// 	user := &auth.User[struct{}]{
// 		Policies: []string{"customers.customers:view", "customers.customers:update"},
// 	}

// 	require.True(t, user.HasPolicy("customers.customers:view"))
// 	require.False(t, user.HasPolicy("customers.customers:delete"))
// }

// func TestUser_HasAnyPolicy(t *testing.T) {
// 	user := &auth.User[struct{}]{
// 		Policies: []string{"customers.customers:view"},
// 	}

// 	require.True(t, user.HasAnyPolicy("customers.customers:view", "admin"))
// 	require.False(t, user.HasAnyPolicy("admin", "super"))
// }

// func TestUser_HasAllPolicies(t *testing.T) {
// 	user := &auth.User[struct{}]{
// 		Policies: []string{"customers.customers:view", "customers.customers:update"},
// 	}

// 	require.True(t, user.HasAllPolicies("customers.customers:view", "customers.customers:update"))
// 	require.False(t, user.HasAllPolicies("customers.customers:view", "customers.customers:delete"))
// }

// --- JWT ---

func TestParseToken_Valid(t *testing.T) {
	token := issueTestToken(t, auth.Claims{
		UserID: 42,
		Email:  "test@example.com",
		Roles:  []string{"admin"},
	})

	claims, err := auth.ParseToken(token, testCfg)
	require.NoError(t, err)
	require.Equal(t, 42, claims.UserID)
	require.Equal(t, "test@example.com", claims.Email)
	require.Equal(t, []string{"admin"}, claims.Roles)
}

func TestParseToken_Empty(t *testing.T) {
	_, err := auth.ParseToken("", testCfg)
	require.ErrorIs(t, err, auth.ErrMissingToken)
}

func TestParseToken_Invalid(t *testing.T) {
	_, err := auth.ParseToken("not.a.token", testCfg)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestParseToken_WrongSecret(t *testing.T) {
	token := issueTestToken(t, auth.Claims{UserID: 1})

	wrongCfg := testCfg
	wrongCfg.SecretKey = "wrong-secret-key-32-bytes-minimum"

	_, err := auth.ParseToken(token, wrongCfg)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestParseToken_Expired(t *testing.T) {
	expiredCfg := auth.TokenConfig{
		SecretKey:     testCfg.SecretKey,
		Issuer:        testCfg.Issuer,
		TokenDuration: -time.Hour, // already expired
	}

	token, err := auth.IssueToken(auth.Claims{UserID: 1}, expiredCfg)
	require.NoError(t, err)

	_, err = auth.ParseToken(token, testCfg)
	require.ErrorIs(t, err, auth.ErrExpiredToken)
}

// --- Policy ---

// func TestIsAuthorized_ExactRole(t *testing.T) {
// 	user := &auth.User[struct{}]{
// 		Policies: []string{"customers.customers:view"},
// 	}

// 	policy := auth.GeneratePolicy("customers.customers", "view")
// 	require.True(t, auth.IsAuthorized(user, policy))
// }

// func TestIsAuthorized_WildcardRole(t *testing.T) {
// 	user := &auth.User[struct{}]{
// 		Policies: []string{"customers.customers:*"},
// 	}

// 	require.True(t, auth.IsAuthorized(user, auth.GeneratePolicy("customers.customers", "view")))
// 	require.True(t, auth.IsAuthorized(user, auth.GeneratePolicy("customers.customers", "delete")))
// }

// func TestIsAuthorized_SuperAdmin(t *testing.T) {
// 	user := &auth.User[struct{}]{
// 		Policies: []string{"super-admin"},
// 	}

// 	// Super-admin can do anything
// 	require.True(t, auth.IsAuthorized(user, auth.GeneratePolicy("any.namespace", "delete")))
// }

// func TestIsAuthorized_NoMatchingRole(t *testing.T) {
// 	user := &auth.User[struct{}]{
// 		Policies: []string{"customers.customers:view"},
// 	}

// 	require.False(t, auth.IsAuthorized(user, auth.GeneratePolicy("customers.customers", "delete")))
// 	require.False(t, auth.IsAuthorized(user, auth.GeneratePolicy("leads.leads", "view")))
// }

func TestGeneratePolicy_String(t *testing.T) {
	p := auth.GeneratePolicy("customers.customers", "view")
	require.Equal(t, "customers.customers:view", p.String())
}
