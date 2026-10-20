//nolint:paralleltest,exhaustruct,exhaustruct_v5,lll,tagliatelle,testpackage,varnamelen,goconst,forcetypeassert,modernize,testifylint,wsl_v5 // tests in the style of the package's others
package validation

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trimLine struct {
	Code string `json:"code" validation:"trim|required"`
}

type trimInput struct {
	Name    string      `json:"name"    validation:"trim|required|max:5"`
	Note    *string     `json:"note"    validation:"trim|required"`
	Opt     *string     `json:"opt"     validation:"trim"`
	Raw     string      `json:"raw"`
	Pass    string      `json:"pass"    validation:"trim"`
	Confirm string      `json:"confirm" validation:"trim|same:pass"`
	Inner   trimLine    `json:"inner"`
	InnerP  *trimLine   `json:"inner_p"`
	Lines   []trimLine  `json:"lines"`
	Ptrs    []*trimLine `json:"ptrs"`
}

func validTrimInput() trimInput {
	return trimInput{Name: "ab", Note: strPtr("x"), Inner: trimLine{Code: "c"}}
}

func TestTrim_StringAndPointer(t *testing.T) {
	in := validTrimInput()
	in.Name, in.Note, in.Opt, in.Raw = "  ab \t", strPtr("  n "), strPtr(" o "), "  keep  "

	require.NoError(t, ValidateInput(&in))
	assert.Equal(t, "ab", in.Name)
	assert.Equal(t, "n", *in.Note)
	assert.Equal(t, "o", *in.Opt)
	assert.Equal(t, "  keep  ", in.Raw, "a field without trim is left alone")
}

func TestTrim_NilPointerStaysNil(t *testing.T) {
	in := validTrimInput()
	require.NoError(t, ValidateInput(&in))
	assert.Nil(t, in.Opt)
}

func TestTrim_SpacesFailRequired(t *testing.T) {
	in := validTrimInput()
	in.Name, in.Note = "   ", strPtr("   ")

	err := ValidateInput(&in)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, "", in.Name)
	assert.Equal(t, "", *in.Note)
	assert.Contains(t, ve.Failures, "name")
	assert.Contains(t, ve.Failures, "note")
}

func TestTrim_MaxCountsTheTrimmedLength(t *testing.T) {
	in := validTrimInput()
	in.Name = "  abcde  "
	require.NoError(t, ValidateInput(&in))

	in.Name = "  abcdef  "
	assert.Error(t, ValidateInput(&in))
}

func TestTrim_CrossFieldRuleSeesTrimmedValues(t *testing.T) {
	in := validTrimInput()
	in.Pass, in.Confirm = " secret", "secret "
	assert.NoError(t, ValidateInput(&in))
}

func TestTrim_Nested(t *testing.T) {
	in := validTrimInput()
	in.Inner.Code = " a "
	in.InnerP = &trimLine{Code: " b "}
	in.Lines = []trimLine{{Code: " c "}, {Code: " d "}}
	in.Ptrs = []*trimLine{{Code: " e "}, nil}

	require.NoError(t, ValidateInput(&in))
	assert.Equal(t, "a", in.Inner.Code)
	assert.Equal(t, "b", in.InnerP.Code)
	assert.Equal(t, "c", in.Lines[0].Code)
	assert.Equal(t, "d", in.Lines[1].Code)
	assert.Equal(t, "e", in.Ptrs[0].Code)
}

func TestTrim_NestedBlankFailsRequiredUnderItsPath(t *testing.T) {
	in := validTrimInput()
	in.Lines = []trimLine{{Code: "ok"}, {Code: "  "}}

	var ve *ValidationError
	require.ErrorAs(t, ValidateInput(&in), &ve)
	assert.Contains(t, ve.Failures, "lines[1].code")
}

func TestTrim_InputPassedByValueIsRefused(t *testing.T) {
	err := ValidateInput(validTrimInput())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pointer")
}

func TestTrimStruct_NotAddressableIsAnError(t *testing.T) {
	err := trimStruct(reflect.ValueOf(trimLine{Code: " a "}), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pointer")
}

func TestCheckRules_Trim(t *testing.T) {
	type onInt struct {
		N int `json:"n" validation:"trim"`
	}
	type onSlice struct {
		S []string `json:"s" validation:"trim"`
	}
	type withIn struct {
		Kind string `json:"kind" validation:"trim|in:a,b"`
	}
	type nested struct {
		Items []onInt `json:"items"`
	}

	for name, v := range map[string]any{"int": onInt{}, "slice": onSlice{}, "nested": nested{}} {
		err := CheckRules(v)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "trim", name)
	}

	err := CheckRules(withIn{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "withIn.Kind")
	assert.Contains(t, err.Error(), "in")

	assert.NoError(t, CheckRules(trimInput{}))
}

func TestRegister_ReachedByValidateInputCheckRulesAndNew(t *testing.T) {
	type input struct {
		Code string `json:"code" validation:"app_only_x"`
	}

	assert.Error(t, CheckRules(input{}), "unknown before Register")

	require.NoError(t, Register("app_only_x", func(_ string, value any, _ string, _ bool, fail func(Failure), _ any) {
		if strings.HasPrefix(value.(string), "bad") {
			fail(Fail("custom"))
		}
	}))
	t.Cleanup(func() {
		appRulesMu.Lock()
		delete(appRules, "app_only_x")
		appRulesMu.Unlock()
	})

	assert.NoError(t, CheckRules(input{}))
	assert.NoError(t, ValidateInput(&input{Code: "good"}))

	var ve *ValidationError
	require.ErrorAs(t, ValidateInput(&input{Code: "bad"}), &ve)
	assert.NoError(t, New().Validate(&input{Code: "good"}))

	assert.Error(t, Register("app_only_x", RequiredRule), "twice")
	assert.Error(t, Register("required", RequiredRule), "a built-in")
}

func TestRequired_PointerToBlankStringIsAbsent(t *testing.T) {
	for _, v := range []string{"", "   "} {
		assert.Len(t, collectFailures(RequiredRule, strPtr(v), "", true), 1, "%q", v)
	}
	assert.Empty(t, collectFailures(RequiredRule, strPtr("x"), "", true))
	// An optional *string "" skips the other rules, as a plain "" does.
	assert.Empty(t, collectFailures(MinRule, strPtr(""), "5", false))
}
