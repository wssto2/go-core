// Package validator runs stateful validation rules against already-populated
// request structs. It complements the binder package:
//
//	binder    → parses HTTP body, coerces types, runs stateless rules
//	           (required, max, min, email, date, in, required_if)
//	validator → runs stateful rules that need DB or gin.Context
//	           (exists, unique, vin, confirmed_password, ...)
//
// Typical handler flow:
//
//	// 1. Bind and run stateless validation
//	request := ctx.MustGet("request").(*CreateCustomerRequest)
//
//	// 2. Run stateful validation (DB checks, etc.)
//	v := validator.New(ctx, db)
//	if err := v.Validate(request); err != nil {
//	    _ = ctx.Error(err)
//	    return
//	}
//
// Custom rules are registered per-application so arv-core stays generic:
//
//	validator.Register("vin", rules.VinRule)
//	validator.Register("exists", rules.ExistsRule)
package validator

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// registry holds the global rule map.
// Rules are registered at startup and read-only during request handling —
// no mutex needed after init.
var registry = map[string]Rule{}

// Register adds a named rule to the global registry.
// Panics if called after the first Validate call (startup-time registration only).
// Call this in your router setup or init() before any requests are handled.
//
// Example:
//
//	func init() {
//	    validator.Register("exists", rules.ExistsRule)
//	    validator.Register("vin",    rules.VinRule)
//	}
func Register(name string, rule Rule) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("validator: rule %q is already registered", name))
	}
	registry[name] = rule
}

// RegisterMany registers multiple rules at once.
// Convenience wrapper around Register.
func RegisterMany(rules map[string]Rule) {
	for name, rule := range rules {
		Register(name, rule)
	}
}

// Validator runs stateful validation rules against a request struct.
// Create one per request via New — it is not safe for concurrent use.
type Validator struct {
	// Errors holds human-readable validation messages keyed by form field name.
	// Returned to the API client.
	Errors map[string][]string

	// DebugErrors holds rule names keyed by form field name.
	// Used in tests via FieldHasError — never sent to end users in production.
	DebugErrors map[string][]string

	ctx *gin.Context
	db  *gorm.DB
}

// New creates a new Validator for the current request.
func New(ctx *gin.Context, db *gorm.DB) *Validator {
	return &Validator{
		Errors:      make(map[string][]string),
		DebugErrors: make(map[string][]string),
		ctx:         ctx,
		db:          db,
	}
}

// Validate runs all registered rules declared in the `validation` struct tags
// of subject. subject must be a non-nil pointer to a struct.
//
// Returns nil if all rules pass.
// Returns ErrValidation with field-level messages if any rule fails.
// Returns ErrUnknownRule if a rule name in a tag has not been registered —
// this is a programming error, not a user error, and should be fixed immediately.
func (v *Validator) Validate(subject any) error {
	rv := reflect.ValueOf(subject)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("validator.Validate: subject must be a non-nil pointer to a struct, got %T", subject)
	}

	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("validator.Validate: subject must be a pointer to a struct, got pointer to %s", rv.Kind())
	}

	rt := rv.Type()

	for i := range rv.NumField() {
		field := rv.Field(i)
		fieldType := rt.Field(i)

		validationTag := fieldType.Tag.Get("validation")
		if validationTag == "" {
			continue
		}

		attribute := fieldType.Tag.Get("form")
		if attribute == "" {
			attribute = fieldType.Name // fallback to Go field name
		}

		parsedRules := parseValidationTag(validationTag)
		isRequired := hasRule(parsedRules, "required")

		for _, r := range parsedRules {
			ruleFunc, ok := registry[r.name]
			if !ok {
				// Unknown rule is a programming error — fail loudly at request time
				// rather than silently skipping the check.
				return ErrUnknownRule{Name: r.name, Field: attribute}
			}

			ruleFunc(
				v.ctx,
				attribute,
				field.Interface(),
				r.args,
				isRequired,
				v.db,
				func(message string) {
					v.Errors[attribute] = append(v.Errors[attribute], message)
					v.DebugErrors[attribute] = append(v.DebugErrors[attribute], r.name)
				},
				subject,
			)
		}
	}

	if len(v.Errors) == 0 {
		return nil
	}

	return ErrValidation{
		Errors:      v.Errors,
		DebugErrors: v.DebugErrors,
	}
}

// HasErrors returns true if any validation errors have been recorded.
func (v *Validator) HasErrors() bool {
	return len(v.Errors) > 0
}

// parsedRule holds a single rule name and its argument string.
type parsedRule struct {
	name string
	args string
}

// parseValidationTag splits "required|exists:users,id|max:30" into parsedRules.
func parseValidationTag(tag string) []parsedRule {
	parts := strings.Split(tag, "|")
	rules := make([]parsedRule, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, args, _ := strings.Cut(part, ":")
		rules = append(rules, parsedRule{
			name: strings.TrimSpace(name),
			args: strings.TrimSpace(args),
		})
	}

	return rules
}

func hasRule(rules []parsedRule, name string) bool {
	for _, r := range rules {
		if r.name == name {
			return true
		}
	}
	return false
}
