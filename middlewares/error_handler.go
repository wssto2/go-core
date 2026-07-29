package middlewares

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/validation"
)

// ErrorHandler is a global middleware that catches all errors attached to the gin context.
func ErrorHandler(log *slog.Logger, translator *i18n.Translator, debug bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Next()

		if len(ctx.Errors) == 0 {
			return
		}

		// We only handle the first error for simplicity
		err := ctx.Errors.Last().Err

		var appErr *apperr.AppError
		if !errors.As(err, &appErr) {
			log.DebugContext(ctx, "Not an app error; wrapping as internal", "error", err)
			// Wrap unknown errors as internal server errors
			appErr = apperr.Internal(err)
		}

		// Store origin for the request logger so it can emit a single unified log line.
		// Only ERROR level is logged here immediately — WARN/INFO are deferred to
		// request logger which has the full request context (status, latency, request_id).
		ctx.Set("error_file", appErr.File)
		ctx.Set("error_line", appErr.Line)

		if appErr.LogLevel == apperr.LevelError {
			log.ErrorContext(ctx, appErr.Message, "error", appErr.Err, "file", appErr.File, "line", appErr.Line)
		}

		// If it's a validation error, include fields
		var valErr *validation.ValidationError
		if errors.As(err, &valErr) {

			locale := ctx.GetString("locale")
			if locale == "" {
				locale = "en"
			}

			translated := make(map[string][]string, len(valErr.Failures))
			if translator != nil {
				for field, failures := range valErr.Failures {
					msgs := make([]string, len(failures))
					for i, f := range failures {
						key := "validation_errors." + string(f.Code)
						msgs[i] = translator.TWith(key, locale, f.Params)
					}
					translated[field] = msgs
				}
			} else {
				for field, failures := range valErr.Failures {
					msgs := make([]string, len(failures))
					for i, f := range failures {
						msgs[i] = fmt.Sprintf("Validation failed: %s", string(f.Code))
					}
					translated[field] = msgs
				}
			}

			resp := gin.H{
				"success": false,
				"message": valErr.Message,
				"errors":  translated,
			}
			if debug {
				resp["debug_errors"] = valErr.DebugFields
			}

			ctx.JSON(apperr.GetHTTPStatus(valErr), resp)
			return
		}

		status := apperr.GetHTTPStatus(appErr)

		message := appErr.Message
		if appErr.Reason != "" && translator != nil {
			locale := ctx.GetString("locale")
			if locale == "" {
				locale = "en"
			}
			key := "errors." + string(appErr.Reason)
			// TWith returns the key itself when no translation exists;
			// keep the original message in that case.
			if translated := translator.TWith(key, locale, appErr.Params); translated != key {
				message = translated
			}
		}

		resp := gin.H{
			"success": false,
			"error":   message,
		}
		if appErr.Reason != "" {
			resp["code"] = appErr.Reason
			if len(appErr.Params) > 0 {
				resp["params"] = appErr.Params
			}
		}
		if len(appErr.Fields) > 0 {
			resp["fields"] = appErr.Fields
		}

		ctx.JSON(status, resp)
	}
}
