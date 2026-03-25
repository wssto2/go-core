package middlewares

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/logger"
	"github.com/wssto2/go-core/validation"
	"github.com/wssto2/go-core/web"
)

// ErrorHandler is a global middleware that catches all errors attached to the gin context.
func ErrorHandler(debug bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Next()

		if len(ctx.Errors) == 0 {
			return
		}

		log := logger.GetFromContext(ctx)
		translator := i18n.GetFromContext(ctx)

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
			log.ErrorContext(ctx, appErr.Message, "error", appErr.Err, "file", appErr.File, "line", appErr.Line)
		case apperr.LevelWarn:
			log.WarnContext(ctx, appErr.Message, "error", appErr.Err)
		case apperr.LevelInfo:
			log.InfoContext(ctx, appErr.Message)
		}

		// If it's a validation error, include fields
		var valErr *validation.ValidationError
		if errors.As(err, &valErr) {

			locale := ctx.GetString("locale")
			if locale == "" {
				locale = "en"
			}

			translated := make(map[string][]string, len(valErr.Failures))
			for field, failures := range valErr.Failures {
				msgs := make([]string, len(failures))
				for i, f := range failures {
					key := "validation_errors." + string(f.Code)
					msgs[i] = translator.TWith(key, locale, f.Params)
				}
				translated[field] = msgs
			}

			resp := gin.H{
				"success": false,
				"message": valErr.Message,
				"errors":  translated,
			}
			if debug {
				resp["debug_errors"] = valErr.DebugFields
			}

			ctx.JSON(web.GetHTTPStatus(valErr), resp)
			return
		}

		status := web.GetHTTPStatus(appErr)

		ctx.JSON(status, gin.H{
			"success": false,
			"error":   appErr.Message,
		})
	}
}
