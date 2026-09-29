package authzhttp

import (
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
)

// DefaultHeader is the header ServiceAccounts reads the key from.
const DefaultHeader = "X-API-Key"

// maxAPIKeyLength bounds what is handed to the key store.
const maxAPIKeyLength = 256

// ServiceAccounts authenticates API-only callers. It reads the API key from
// header (DefaultHeader when empty), validates it through go-core's
// existing key store (hashed, revocable, with optional expiry) and sets the
// principal to the service account that owns the key.
//
// auth.APIKey.UserID is the owning service account's ID: a key belongs to
// exactly one service account, which gets roles at a scope like a person.
// Service accounts never match the Own qualifier and are audited as themselves.
func ServiceAccounts(store auth.APIKeyStore, header string) gin.HandlerFunc {
	if header == "" {
		header = DefaultHeader
	}
	return func(ctx *gin.Context) {
		raw := ctx.GetHeader(header)
		if raw == "" || len(raw) > maxAPIKeyLength {
			abort(ctx, apperr.Unauthorized("invalid API key"))
			return
		}
		key, err := auth.ValidateAPIKey(ctx.Request.Context(), store, raw)
		if err != nil || key.UserID <= 0 {
			abort(ctx, apperr.Unauthorized("invalid API key"))
			return
		}
		ctx.Request = ctx.Request.WithContext(authz.WithPrincipal(ctx.Request.Context(), authz.ServiceAccount(key.UserID)))
		ctx.Next()
	}
}
