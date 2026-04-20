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
	"sync"

	"github.com/wssto2/go-core/apperr"
)

type Validator struct {
	mu          sync.RWMutex
	Errors      map[string][]Failure
	DebugErrors map[string][]string
	registry    map[string]Rule // protected by mu
}

var defaultRegistry = map[string]Rule{
	"required":    RequiredRule,
	"required_if": RequiredIfRule,
	"email":       EmailRule,
	"min":         MinRule,
	"max":         MaxRule,
	"in":          InRule,
	"date":        DateRule,
	"date_time":   DateTimeRule,
	"year":        YearRule,
	"month":       MonthRule,
	"password":    PasswordRule,
	"confirmed":   ConfirmedRule,
	"len":         LenRule,
	"between":     BetweenRule,
	"same":        SameRule,
	"different":   DifferentRule,
	"url":         URLRule,
	"uuid":        UUIDRule,
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
	v.mu.Lock()
	defer v.mu.Unlock()

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
	v.mu.Lock()
	defer v.mu.Unlock()

	v.registry[name] = rule
}

func (v *Validator) Validate(subject any) error {
	rv := reflect.ValueOf(subject)
	if !rv.IsValid() || rv.Kind() != reflect.Ptr || rv.IsNil() {
		return apperr.BadRequest(fmt.Sprintf("validator.Validate: subject must be a non-nil pointer to a struct, got %T", subject))
	}

	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return apperr.BadRequest(fmt.Sprintf("validator.Validate: subject must be a non-nil pointer to a struct, got %T", subject))
	}

	v.mu.RLock()
	registry := make(map[string]Rule, len(v.registry))
	for name, rule := range v.registry {
		registry[name] = rule
	}
	v.mu.RUnlock()

	errorsMap := make(map[string][]Failure)
	debugErrorsMap := make(map[string][]string)

	if err := v.validateFields(rv, registry, subject, errorsMap, debugErrorsMap, ""); err != nil {
		return err
	}

	v.mu.Lock()
	v.Errors = errorsMap
	v.DebugErrors = debugErrorsMap
	v.mu.Unlock()

	if len(errorsMap) == 0 {
		return nil
	}

	return NewValidationError("validation failed", cloneFailuresMap(errorsMap), cloneDebugFieldsMap(debugErrorsMap))
}

// validateFields iterates the exported fields of rv, applying validation tags
// and recursing into nested structs and slice/array elements.
// prefix is prepended to every attribute key (e.g. "items[0].").
// subject is the struct pointer whose fields are being validated; it is used by
// cross-field rules (required_if, same, different, confirmed) to look up siblings.
func (v *Validator) validateFields(rv reflect.Value, registry map[string]Rule, subject any, errorsMap map[string][]Failure, debugErrorsMap map[string][]string, prefix string) error {
	meta := globalMetaCache.get(rv.Type())

	for _, fm := range meta.Fields {
		field := rv.FieldByIndex(fm.FieldIndex)
		attribute := prefix + fm.Alias

		if len(fm.Rules) > 0 {
			isRequired := hasRule(fm.Rules, "required") || isRequiredIfConditionMet(fm.Rules, attribute, subject)

			for _, r := range fm.Rules {
				ruleFunc, ok := registry[r.name]
				if !ok {
					return NewErrUnknownRule(r.name, attribute)
				}

				var ruleErr error
				func() {
					defer func() {
						if recovered := recover(); recovered != nil {
							switch recoveredValue := recovered.(type) {
							case error:
								ruleErr = recoveredValue
							default:
								ruleErr = fmt.Errorf("validator: rule %q on field %q panicked: %v", r.name, attribute, recovered)
							}
						}
					}()

					ruleFunc(
						attribute,
						field.Interface(),
						r.args,
						isRequired,
						func(f Failure) {
							errorsMap[attribute] = append(errorsMap[attribute], f)
							debugErrorsMap[attribute] = append(debugErrorsMap[attribute], r.name)
						},
						subject,
					)
				}()

				if ruleErr != nil {
					return apperr.Internal(ruleErr)
				}
			}
		}

		// Only recurse when the cache indicates this field leads to a struct.
		if fm.ElemType != nil {
			if err := v.validateNestedField(field, registry, errorsMap, debugErrorsMap, attribute); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateNestedField recurses into struct fields and slice/array elements that
// are themselves structs, collecting validation errors with a dotted prefix.
func (v *Validator) validateNestedField(fv reflect.Value, registry map[string]Rule, errorsMap map[string][]Failure, debugErrorsMap map[string][]string, prefix string) error {
	if fv.Kind() == reflect.Ptr {
		if fv.IsNil() {
			return nil
		}
		fv = fv.Elem()
	}

	switch fv.Kind() {
	case reflect.Struct:
		return v.validateStructValue(fv, registry, errorsMap, debugErrorsMap, prefix+".")

	case reflect.Slice, reflect.Array:
		for i := 0; i < fv.Len(); i++ {
			elem := fv.Index(i)
			if elem.Kind() == reflect.Ptr {
				if elem.IsNil() {
					continue
				}
				elem = elem.Elem()
			}
			if elem.Kind() == reflect.Struct {
				elemPrefix := fmt.Sprintf("%s[%d].", prefix, i)
				if err := v.validateStructValue(elem, registry, errorsMap, debugErrorsMap, elemPrefix); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// validateStructValue validates a struct reflect.Value, building the subject
// pointer needed by cross-field rules.
func (v *Validator) validateStructValue(sv reflect.Value, registry map[string]Rule, errorsMap map[string][]Failure, debugErrorsMap map[string][]string, prefix string) error {
	meta := globalMetaCache.get(sv.Type())
	if !meta.HasAnyValidation {
		return nil
	}

	var nestedSubject any
	if sv.CanAddr() {
		nestedSubject = sv.Addr().Interface()
	} else {
		ptr := reflect.New(sv.Type())
		ptr.Elem().Set(sv)
		nestedSubject = ptr.Interface()
	}

	return v.validateFields(sv, registry, nestedSubject, errorsMap, debugErrorsMap, prefix)
}

func (v *Validator) GetErrors() map[string][]Failure {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return cloneFailuresMap(v.Errors)
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

// isRequiredIfConditionMet returns true when any required_if rule in the list
// has its condition met. When args are malformed or the referenced field is
// missing (programming errors), it returns true so RequiredIfRule will still
// panic with the appropriate error.
func isRequiredIfConditionMet(rules []parsedRule, attribute string, subject any) bool {
	for _, r := range rules {
		if r.name != "required_if" {
			continue
		}
		otherField, expectedValue, err := parseRequiredIfArgs(attribute, r.args)
		if err != nil {
			// Malformed config — return true so RequiredIfRule panics as designed.
			return true
		}
		otherValue, found, err := lookupSubjectFieldValue(subject, otherField)
		if err != nil || !found {
			// Missing field — return true so RequiredIfRule panics as designed.
			return true
		}
		if fmt.Sprintf("%v", otherValue) == expectedValue {
			return true
		}
	}
	return false
}


func cloneFailuresMap(src map[string][]Failure) map[string][]Failure {
	dst := make(map[string][]Failure, len(src))
	for key, failures := range src {
		copied := make([]Failure, len(failures))
		for i, failure := range failures {
			copied[i] = Failure{
				Code:   failure.Code,
				Params: cloneParamsMap(failure.Params),
			}
		}
		dst[key] = copied
	}
	return dst
}

func cloneDebugFieldsMap(src map[string][]string) map[string][]string {
	dst := make(map[string][]string, len(src))
	for key, values := range src {
		copied := make([]string, len(values))
		copy(copied, values)
		dst[key] = copied
	}
	return dst
}

func cloneParamsMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
