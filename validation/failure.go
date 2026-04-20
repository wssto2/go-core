package validation

type RuleCode string

const (
	CodeRequired   RuleCode = "required"
	CodeRequiredIf RuleCode = "required_if"
	CodeEmail      RuleCode = "email"
	CodeMax        RuleCode = "max"
	CodeMin        RuleCode = "min"
	CodeIn         RuleCode = "in"
	CodeDate       RuleCode = "date"
	CodeYear       RuleCode = "year"
	CodeMonth      RuleCode = "month"
	CodePassword   RuleCode = "password"
	CodeConfirmed  RuleCode = "confirmed"

	CodeLen       RuleCode = "len"
	CodeBetween   RuleCode = "between"
	CodeSame      RuleCode = "same"
	CodeDifferent RuleCode = "different"
	CodeURL       RuleCode = "url"
	CodeUUID      RuleCode = "uuid"

	// coerce codes — type mismatches from request parsing
	CodeInvalidType          RuleCode = "invalid_type" // generic fallback
	CodeMustBeNumber         RuleCode = "must_be_number"
	CodeMustBePositiveNumber RuleCode = "must_be_positive_number"
	CodeMustBeString         RuleCode = "must_be_string"
	CodeMustBeBoolean        RuleCode = "must_be_boolean"
	CodeMustBeList           RuleCode = "must_be_list"
	CodeMustBeObject         RuleCode = "must_be_object"
)

type Failure struct {
	Code   RuleCode       // "max"
	Params map[string]any // {"max": 10}  — nil for parameterless rules
}

func Fail(code RuleCode) Failure {
	return Failure{Code: code}
}

func FailWith(code RuleCode, params map[string]any) Failure {
	return Failure{Code: code, Params: params}
}
