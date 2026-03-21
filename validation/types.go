package validation

// ValidationContext provides ambient context to rules, without coupling to gin.
type ValidationContext interface {
	Locale() string
	// Extend as needed: RequestID(), UserID(), etc.
}

// Rule is a stateless validation function.
type Rule func(
	ctx ValidationContext,
	attribute string,
	value any,
	args string,
	required bool,
	fail func(message string),
	subject any,
)
