// Package validation provides a lightweight, struct-tag-driven input validation
// system for request types.
//
// Rules are declared via the "validation" struct tag:
//
//	type CreateUserRequest struct {
//	    Email string `json:"email" validation:"required|email"`
//	    Age   int    `json:"age"   validation:"min:18|max:120"`
//	}
//
// Validate a request with:
//
//	v := validation.New()
//	if err := v.Validate(&req); err != nil {
//	    // err is *ValidationError with per-field failures
//	}
//
// Custom rules can be registered per-validator instance via Register or
// globally shared via NewWithRules.
package validation

import (
	"fmt"
	"reflect"
	"strings"
)

type Validator struct {
	Errors      map[string][]Failure
	DebugErrors map[string][]string
	registry    map[string]Rule // per-instance, not global
}

var defaultRegistry = map[string]Rule{
	"required":  RequiredRule,
	"email":     EmailRule,
	"min":       MinRule,
	"max":       MaxRule,
	"in":        InRule,
	"date":      DateRule,
	"date_time": DateTimeRule,
}

func New() *Validator {
	return NewWithRules(nil)
}

// NewWithRules creates a Validator with custom rules merged on top of the defaults.
func NewWithRules(extra map[string]Rule) *Validator {
	r := make(map[string]Rule, len(defaultRegistry)+len(extra))
	for k, v := range defaultRegistry {
		r[k] = v
	}
	for k, v := range extra {
		r[k] = v
	}
	return &Validator{
		registry:    r,
		Errors:      make(map[string][]Failure),
		DebugErrors: make(map[string][]string),
	}
}

// Register adds a rule. Returns an error if the rule name is already registered.
// To intentionally replace a rule, use RegisterOverride.
func (v *Validator) Register(name string, rule Rule) error {
	if _, exists := v.registry[name]; exists {
		return fmt.Errorf("validator: rule %q is already registered — use RegisterOverride to replace it", name)
	}
	v.registry[name] = rule
	return nil
}

// MustRegister adds a rule. Panics if the rule name is already registered.
// To intentionally replace a rule, use RegisterOverride.
func (v *Validator) MustRegister(name string, rule Rule) {
	if err := v.Register(name, rule); err != nil {
		panic(err)
	}
}

// RegisterOverride adds or replaces a rule without panicking on duplicates.
func (v *Validator) RegisterOverride(name string, rule Rule) {
	v.registry[name] = rule
}

func (v *Validator) Validate(subject any) error {
	rv := reflect.ValueOf(subject)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("validator.Validate: subject must be a non-nil pointer to a struct, got %T", subject)
	}

	rv = rv.Elem()
	rt := rv.Type()

	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		fieldType := rt.Field(i)

		validationTag := fieldType.Tag.Get("validation")
		if validationTag == "" {
			continue
		}

		attribute := fieldType.Tag.Get("form")
		if attribute == "" {
			attribute = fieldType.Name
		}

		parsedRules := parseValidationTag(validationTag)
		isRequired := hasRule(parsedRules, "required")

		for _, r := range parsedRules {
			ruleFunc, ok := v.registry[r.name]
			if !ok {
				return NewErrUnknownRule(r.name, attribute)
			}

			ruleFunc(
				attribute,
				field.Interface(),
				r.args,
				isRequired,
				func(f Failure) {
					v.Errors[attribute] = append(v.Errors[attribute], f)
					v.DebugErrors[attribute] = append(v.DebugErrors[attribute], r.name)
				},
				subject,
			)
		}
	}

	if len(v.Errors) == 0 {
		return nil
	}

	return NewValidationError("validation failed", v.Errors, v.DebugErrors)
}

func (v *Validator) GetErrors() map[string][]Failure {
	return v.Errors
}

type parsedRule struct {
	name string
	args string
}

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
