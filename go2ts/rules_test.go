package go2ts

import (
	"strings"
	"testing"
)

type unknownRuleInput struct {
	Name string `json:"name" validation:"required|exists:users,name|max:20"`
}

type serverOnlyInput struct {
	Password string `json:"password" validation:"required|password|max:20"`
	Again    string `json:"again"    validation:"same:password|max:20"`
	Kind     int    `json:"kind"     validation:"max:9"`
	Extra    string `json:"extra"    validation:"required_if:kind,1|confirmed:again|different:password|max:20"`
}

type declaredRuleInput struct {
	Code string `json:"code" validation:"unique_for_test|max:5"`
}

func TestGenerateSchemas_UnknownRuleFailsNamingStructFieldAndRule(t *testing.T) {
	err := GenerateSchemas([]any{unknownRuleInput{}}, t.TempDir())
	if err == nil {
		t.Fatal("an unknown validation rule generated a schema that silently lacks it")
	}

	for _, want := range []string{"unknownRuleInput", "Name", `"exists"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %s", err, want)
		}
	}
}

func TestGenerateSchemas_ServerOnlyRulesAreNotAnError(t *testing.T) {
	if err := GenerateSchemas([]any{serverOnlyInput{}}, t.TempDir()); err != nil {
		t.Fatalf("the validator's cross-field and password rules have no Zod form and are declared server-only: %v", err)
	}
}

func TestServerOnly_DeclaresAnApplicationsOwnRule(t *testing.T) {
	if err := GenerateSchemas([]any{declaredRuleInput{}}, t.TempDir()); err == nil {
		t.Fatal("the rule is not declared yet")
	}

	ServerOnly("unique_for_test")

	if err := GenerateSchemas([]any{declaredRuleInput{}}, t.TempDir()); err != nil {
		t.Fatalf("a declared rule is server-only: %v", err)
	}
}

func TestFieldToZodExpr_InStringIsAnEnum(t *testing.T) {
	for tag, want := range map[string]string{
		"in:foo,bar":      `z.enum(["foo", "bar"])`,
		"required|in:one": `z.enum(["one"])`,
		"in:1,2":          `z.enum(["1", "2"])`,
	} {
		if expr, _ := fieldToZodExpr(stringType, tag); expr != want {
			t.Errorf("%s: got %q, want %q", tag, expr, want)
		}
	}
}

func TestFieldToZodExpr_YearAndMonth(t *testing.T) {
	for tag, want := range map[string]string{
		"year":           `z.string().regex(/^\d{4}$/).or(z.literal(''))`,
		"required|year":  `z.string().min(1).regex(/^\d{4}$/)`,
		"required|month": `z.string().min(1).regex(/^(0?[1-9]|1[0-2])$/)`,
	} {
		if expr, _ := fieldToZodExpr(stringType, tag); expr != want {
			t.Errorf("%s: got %q, want %q", tag, expr, want)
		}
	}
}
