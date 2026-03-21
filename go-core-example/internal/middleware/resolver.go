package middleware

import (
	"go-core-example/internal/domain/auth"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	coreauth "github.com/wssto2/go-core/auth"
	"gorm.io/gorm"
)

// userResolver implements auth.Resolver.
// After the JWT is validated, Resolve loads the full user from the DB so that
// the handler has access to live roles, permissions, and app-specific data.
type userResolver struct {
	db *gorm.DB
}

// NewUserResolver constructs an auth.Resolver backed by the given *gorm.DB.
func NewUserResolver(db *gorm.DB) coreauth.Resolver {
	return coreauth.ResolverFunc(func(ctx *gin.Context, claims *coreauth.Claims) (coreauth.Identifiable, error) {
		var user auth.AppUser

		err := db.WithContext(ctx.Request.Context()).
			Where("id = ? AND active = 1", claims.UserID).
			First(&user).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, apperr.Unauthorized("user account not found")
			}

			return nil, apperr.Internal(err)
		}

		// Populate policies from the DB record onto the embedded User.
		user.User.Policies = user.Policies

		return &user.User, nil
	})
}
