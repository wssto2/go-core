package middlewares

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/logger"
	"github.com/wssto2/go-core/validation"
)

// ErrorHandler is a global middleware that catches all errors attached to the gin context.
func ErrorHandler(debug bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Next()

		if len(ctx.Errors) == 0 {
			return
		}

		// We only handle the first error for simplicity
		err := ctx.Errors.Last().Err

		var appErr *apperr.AppError
		if !errors.As(err, &appErr) {
			// Wrap unknown errors as internal server errors
			appErr = apperr.Internal(err)
		}

		// Log based on the error's log level
		switch appErr.LogLevel {
		case apperr.LevelError:
			logger.Log.ErrorContext(ctx, appErr.Message, "error", appErr.Err, "file", appErr.File, "line", appErr.Line)
		case apperr.LevelWarn:
			logger.Log.WarnContext(ctx, appErr.Message, "error", appErr.Err)
		case apperr.LevelInfo:
			logger.Log.InfoContext(ctx, appErr.Message)
		}

		// If it's a validation error, include fields
		var valErr *validation.ValidationError
		if errors.As(err, &valErr) {
			resp := gin.H{
				"success": false,
				"message": valErr.Message,
				"errors":  valErr.Errors,
			}
			if debug {
				resp["debug_errors"] = valErr.DebugFields
			}
			ctx.JSON(valErr.StatusCode, resp)
			return
		}

		ctx.JSON(appErr.StatusCode, gin.H{
			"success": false,
			"error":   appErr.Message,
		})
	}
}
