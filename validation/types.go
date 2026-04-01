package validation

// Rule is a stateless validation function.
type Rule func(
	attribute string,
	value any,
	args string,
	required bool,
	fail func(Failure),
	subject any,
)
