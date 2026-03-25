package middlewares

import (
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/binders"
	"github.com/wssto2/go-core/validation"
)

const requestKey = "go-core.middlewares.request"

// BindRequest returns a middleware that:
// 1. Parses and coerces the request body into T using binders.
// 2. Runs stateless validation using the core validator.
// 3. Stores the result in context key "request".
func BindRequest[T any]() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request T

		// Step 1: Parsing & Coercion & Stateless validation (binders level)
		if err := binders.BindJSON(ctx, &request); err != nil {
			_ = ctx.Error(err)
			ctx.Abort()
			return
		}

		// Step 2: Pure Syntactic Validation (struct tags)
		v := validation.New()
		if err := v.Validate(&request); err != nil {
			_ = ctx.Error(err)
			ctx.Abort()
			return
		}

		ctx.Set(requestKey, &request)
		ctx.Next()
	}
}

func GetRequest[T any](ctx *gin.Context) (*T, bool) {
	val, ok := ctx.Get(requestKey)
	if !ok {
		return nil, false
	}
	req, ok := val.(*T)
	return req, ok
}

func MustGetRequest[T any](ctx *gin.Context) *T {
	req, ok := GetRequest[T](ctx)
	if !ok {
		panic("middlewares.MustGetRequest: request not found in context")
	}
	return req
}
