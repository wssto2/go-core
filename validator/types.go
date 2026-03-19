package validator

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Rule is a stateful validation function with full access to the HTTP context,
// database, and the complete subject struct.
//
// Use this for rules that cannot be evaluated from the field value alone:
//   - "exists"             → queries the DB to verify a foreign key exists
//   - "vin"                → may call an external service
//   - "confirmed_password" → reads another field from subject
//   - "unique"             → queries the DB for uniqueness
//
// For simple stateless rules (required, max, email, date, in) those are
// handled by the binder during request parsing — they never reach the validator.
//
// Parameters:
//
//	ctx       — gin request context (access headers, session, etc.)
//	attribute — the form field name (from the `form` struct tag)
//	value     — the current field value, already coerced to its Go type
//	args      — the rule parameter string, e.g. "users,id" for exists:users,id
//	required  — true if the field has the "required" rule (lets rules skip empty optional fields)
//	db        — database connection for the current request
//	fail      — call this to record a validation error for this field
//	subject   — pointer to the full request struct (for cross-field rules)
type Rule func(
	ctx *gin.Context,
	attribute string,
	value any,
	args string,
	required bool,
	db *gorm.DB,
	fail func(message string),
	subject any,
)
