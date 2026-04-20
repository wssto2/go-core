package go2ts

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// ── parseValidationTag ───────────────────────────────────────────────────────

func TestParseValidationTag_Empty(t *testing.T) {
	rules := parseValidationTag("")
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
}

func TestParseValidationTag_SingleRule(t *testing.T) {
	rules := parseValidationTag("required")
	if len(rules) != 1 || rules[0].name != "required" || rules[0].args != "" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
}

func TestParseValidationTag_RuleWithArgs(t *testing.T) {
	rules := parseValidationTag("max:50")
	if len(rules) != 1 || rules[0].name != "max" || rules[0].args != "50" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
}

func TestParseValidationTag_MultipleRules(t *testing.T) {
	rules := parseValidationTag("required|max:50|email")
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}
	if rules[0].name != "required" || rules[1].name != "max" || rules[1].args != "50" || rules[2].name != "email" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
}

func TestParseValidationTag_CrossFieldRule(t *testing.T) {
	rules := parseValidationTag("required_if:vrsta,1|max:30")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].name != "required_if" || rules[0].args != "vrsta,1" {
		t.Fatalf("unexpected cross-field rule: %+v", rules[0])
	}
	if rules[1].name != "max" || rules[1].args != "30" {
		t.Fatalf("unexpected max rule: %+v", rules[1])
	}
}

// ── fieldToZodExpr ───────────────────────────────────────────────────────────

func TestFieldToZodExpr_RequiredString(t *testing.T) {
	expr, crossField := fieldToZodExpr(stringType, "required|max:50")
	if expr != "z.string().min(1).max(50)" {
		t.Errorf("got %q", expr)
	}
	if crossField {
		t.Error("expected no cross-field rules")
	}
}

func TestFieldToZodExpr_OptionalEmail(t *testing.T) {
	// Non-required email: must allow empty string (mirrors Go validator skip behaviour).
	expr, _ := fieldToZodExpr(stringType, "email|max:50")
	if expr != "z.string().email().max(50).or(z.literal(''))" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_RequiredEmail(t *testing.T) {
	expr, _ := fieldToZodExpr(stringType, "required|email|max:50")
	if expr != "z.string().min(1).email().max(50)" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_OptionalURL(t *testing.T) {
	expr, _ := fieldToZodExpr(stringType, "url")
	if expr != "z.string().url().or(z.literal(''))" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_RequiredUUID(t *testing.T) {
	expr, _ := fieldToZodExpr(stringType, "required|uuid")
	if expr != "z.string().min(1).uuid()" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_Between(t *testing.T) {
	expr, _ := fieldToZodExpr(intType, "between:1,100")
	if expr != "z.number().int().min(1).max(100)" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_Len(t *testing.T) {
	expr, _ := fieldToZodExpr(stringType, "len:10")
	if expr != "z.string().length(10)" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_InNumeric(t *testing.T) {
	expr, _ := fieldToZodExpr(intType, "required|in:1,2")
	if expr != "z.union([z.literal(1), z.literal(2)])" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_InSingleValue(t *testing.T) {
	expr, _ := fieldToZodExpr(intType, "in:42")
	if expr != "z.literal(42)" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_InString(t *testing.T) {
	expr, _ := fieldToZodExpr(stringType, "in:foo,bar")
	if expr != `z.union([z.literal("foo"), z.literal("bar")])` {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_NullablePointerString(t *testing.T) {
	expr, _ := fieldToZodExpr(ptrStringType, "max:50")
	if expr != "z.string().max(50).nullable().optional()" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_RequiredNullablePointer(t *testing.T) {
	// required + pointer → nullable but NOT optional.
	expr, _ := fieldToZodExpr(ptrStringType, "required|max:50")
	if expr != "z.string().min(1).max(50).nullable()" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_CrossFieldRuleMarked(t *testing.T) {
	_, hasCrossField := fieldToZodExpr(stringType, "required_if:vrsta,1|max:30")
	if !hasCrossField {
		t.Error("expected hasCrossField=true")
	}
}

func TestFieldToZodExpr_CrossFieldRuleExprStillValid(t *testing.T) {
	// cross-field rules are skipped but other rules still apply.
	expr, _ := fieldToZodExpr(stringType, "required_if:vrsta,1|max:30")
	if expr != "z.string().max(30)" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_NoValidationTag(t *testing.T) {
	expr, crossField := fieldToZodExpr(stringType, "")
	if expr != "z.string()" {
		t.Errorf("got %q", expr)
	}
	if crossField {
		t.Error("expected no cross-field rules")
	}
}

func TestFieldToZodExpr_BoolNoRules(t *testing.T) {
	expr, _ := fieldToZodExpr(boolType, "")
	if expr != "z.boolean()" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_DateOptional(t *testing.T) {
	expr, _ := fieldToZodExpr(stringType, "date")
	if expr != "z.string().date().or(z.literal(''))" {
		t.Errorf("got %q", expr)
	}
}

func TestFieldToZodExpr_DateRequired(t *testing.T) {
	expr, _ := fieldToZodExpr(stringType, "required|date")
	if expr != "z.string().min(1).date()" {
		t.Errorf("got %q", expr)
	}
}

// ── structToZod ──────────────────────────────────────────────────────────────

type SimpleRequest struct {
	Name  string `json:"name"  validation:"required|max:50"`
	Email string `json:"email" validation:"email|max:100"`
}

type RequestWithIn struct {
	Status int `json:"status" validation:"required|in:1,2,3"`
}

type RequestWithCrossField struct {
	Vrsta int    `json:"vrsta" validation:"required|in:1,2"`
	Ime   string `json:"ime"   validation:"required_if:vrsta,1|max:30"`
}

type RequestWithNullable struct {
	Note *string `json:"note" validation:"max:200"`
}

type NestedChild struct {
	City string `json:"city" validation:"required|max:50"`
}

type RequestWithNested struct {
	Name    string      `json:"name"    validation:"required|max:50"`
	Address NestedChild `json:"address"`
}

func TestStructToZod_SimpleRequest(t *testing.T) {
	ctx := &GenContext{Processed: make(map[string]bool)}
	name, output, children, err := structToZod(SimpleRequest{}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if name != "SimpleRequest" {
		t.Errorf("unexpected name: %q", name)
	}
	if len(children) != 0 {
		t.Errorf("expected no children, got %d", len(children))
	}
	if !strings.Contains(output, "import { z } from 'zod';") {
		t.Error("missing zod import")
	}
	if !strings.Contains(output, "export const SimpleRequestSchema = z.object({") {
		t.Error("missing schema constant")
	}
	if !strings.Contains(output, "export type SimpleRequest = z.infer<typeof SimpleRequestSchema>;") {
		t.Error("missing inferred type")
	}
	if !strings.Contains(output, "name: z.string().min(1).max(50),") {
		t.Errorf("wrong name field, output:\n%s", output)
	}
	if !strings.Contains(output, "email: z.string().email().max(100).or(z.literal('')),") {
		t.Errorf("wrong email field, output:\n%s", output)
	}
}

func TestStructToZod_InRule(t *testing.T) {
	ctx := &GenContext{Processed: make(map[string]bool)}
	_, output, _, err := structToZod(RequestWithIn{}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "status: z.union([z.literal(1), z.literal(2), z.literal(3)]),") {
		t.Errorf("wrong status field, output:\n%s", output)
	}
}

func TestStructToZod_CrossFieldGeneratesComment(t *testing.T) {
	ctx := &GenContext{Processed: make(map[string]bool)}
	_, output, _, err := structToZod(RequestWithCrossField{}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "cross-field rules") {
		t.Errorf("expected cross-field comment, output:\n%s", output)
	}
	if !strings.Contains(output, "superRefine") {
		t.Errorf("expected superRefine hint, output:\n%s", output)
	}
	// The field itself still gets the generatable rules.
	if !strings.Contains(output, "ime: z.string().max(30),") {
		t.Errorf("wrong ime field, output:\n%s", output)
	}
}

func TestStructToZod_NullableField(t *testing.T) {
	ctx := &GenContext{Processed: make(map[string]bool)}
	_, output, _, err := structToZod(RequestWithNullable{}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "note: z.string().max(200).nullable().optional(),") {
		t.Errorf("wrong note field, output:\n%s", output)
	}
}

func TestStructToZod_NestedStructCollectedAsChild(t *testing.T) {
	ctx := &GenContext{Processed: make(map[string]bool)}
	_, output, children, err := structToZod(RequestWithNested{}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := children["NestedChild"]; !ok {
		t.Error("NestedChild not collected as child")
	}
	if !strings.Contains(output, "import { NestedChildSchema } from './NestedChild';") {
		t.Errorf("missing child import, output:\n%s", output)
	}
	if !strings.Contains(output, "address: NestedChildSchema,") {
		t.Errorf("wrong address field, output:\n%s", output)
	}
}

func TestStructToZod_DeduplicatesAlreadyProcessed(t *testing.T) {
	ctx := &GenContext{Processed: make(map[string]bool)}
	ctx.Processed["SimpleRequest"] = true
	name, output, _, err := structToZod(SimpleRequest{}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if name != "" || output != "" {
		t.Error("expected empty output for already-processed type")
	}
}

func TestStructToZod_ErrorOnNonStruct(t *testing.T) {
	ctx := &GenContext{Processed: make(map[string]bool)}
	_, _, _, err := structToZod("not a struct", ctx)
	if err == nil {
		t.Fatal("expected error for non-struct")
	}
}

func TestStructToZod_AutoGeneratedComment(t *testing.T) {
	ctx := &GenContext{Processed: make(map[string]bool)}
	_, output, _, err := structToZod(SimpleRequest{}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "// This file is auto-generated. Do not edit manually.") {
		t.Error("missing auto-generated comment")
	}
}

// ── GenerateSchemas ──────────────────────────────────────────────────────────

func TestGenerateSchemas_WritesFiles(t *testing.T) {
	dir := t.TempDir()
	err := GenerateSchemas([]interface{}{SimpleRequest{}}, dir)
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(dir + "/SimpleRequest.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "SimpleRequestSchema") {
		t.Error("generated file missing SimpleRequestSchema")
	}
}

func TestGenerateSchemas_CreatesOutputDirectory(t *testing.T) {
	dir := t.TempDir() + "/nested/dir"
	err := GenerateSchemas([]interface{}{SimpleRequest{}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir + "/SimpleRequest.ts"); os.IsNotExist(err) {
		t.Error("expected file to be created")
	}
}

func TestGenerateSchemas_RecursivelyProcessesChildren(t *testing.T) {
	dir := t.TempDir()
	err := GenerateSchemas([]interface{}{RequestWithNested{}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir + "/RequestWithNested.ts"); os.IsNotExist(err) {
		t.Error("missing RequestWithNested.ts")
	}
	if _, err := os.Stat(dir + "/NestedChild.ts"); os.IsNotExist(err) {
		t.Error("missing NestedChild.ts — nested structs must be generated recursively")
	}
}

func TestGenerateSchemas_DeduplicatesNestedTypes(t *testing.T) {
	type Shared struct {
		Label string `json:"label" validation:"required|max:20"`
	}
	type ReqA struct {
		A Shared `json:"a"`
	}
	type ReqB struct {
		B Shared `json:"b"`
	}

	dir := t.TempDir()
	err := GenerateSchemas([]interface{}{ReqA{}, ReqB{}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	// Shared should appear only once.
	if _, err := os.Stat(dir + "/Shared.ts"); os.IsNotExist(err) {
		t.Error("missing Shared.ts")
	}
}

func TestGenerateSchemas_ErrorOnNonStruct(t *testing.T) {
	dir := t.TempDir()
	err := GenerateSchemas([]interface{}{"not a struct"}, dir)
	if err == nil {
		t.Fatal("expected error for non-struct input")
	}
}

// ── UpdateCustomerRequest-like integration test ──────────────────────────────

type UpdateCustomerRequestTest struct {
	Vrsta   int    `json:"vrsta"   validation:"required|in:1,2"`
	Ime     string `json:"ime"     validation:"required_if:vrsta,1|max:30"`
	Prezime string `json:"prezime" validation:"required|max:50"`
	Email   string `json:"email"   validation:"email|max:50"`
	Grad    string `json:"grad"    validation:"required|max:50"`
	Gsm     string `json:"gsm"     validation:"required|max:30"`
	Website string `json:"website" validation:"url"`
}

func TestGenerateSchemas_UpdateCustomerRequest(t *testing.T) {
	dir := t.TempDir()
	err := GenerateSchemas([]interface{}{UpdateCustomerRequestTest{}}, dir)
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(dir + "/UpdateCustomerRequestTest.ts")
	if err != nil {
		t.Fatal(err)
	}
	out := string(content)

	cases := []struct {
		field    string
		wantExpr string
	}{
		{"vrsta", "vrsta: z.union([z.literal(1), z.literal(2)]),"},
		{"prezime", "prezime: z.string().min(1).max(50),"},
		{"email", "email: z.string().email().max(50).or(z.literal('')),"},
		{"grad", "grad: z.string().min(1).max(50),"},
		{"gsm", "gsm: z.string().min(1).max(30),"},
		{"website", "website: z.string().url().or(z.literal('')),"},
	}

	for _, tc := range cases {
		if !strings.Contains(out, tc.wantExpr) {
			t.Errorf("field %s: want %q\nfull output:\n%s", tc.field, tc.wantExpr, out)
		}
	}

	// ime has required_if — should produce a cross-field comment and only max:30.
	if !strings.Contains(out, "ime: z.string().max(30),") {
		t.Errorf("wrong ime field\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "cross-field rules") {
		t.Errorf("missing cross-field comment\nfull output:\n%s", out)
	}
}

// ── reflect helpers ──────────────────────────────────────────────────────────

var (
	stringType    = reflect.TypeOf("")
	ptrStringType = reflect.TypeOf((*string)(nil))
	intType       = reflect.TypeOf(0)
	boolType      = reflect.TypeOf(false)
)
