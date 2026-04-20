package validation_test

import (
	"testing"

	"github.com/wssto2/go-core/validation"
)

// ─────────────────────────────────────────────────────────────────────────────
// Benchmark subjects
// ─────────────────────────────────────────────────────────────────────────────

type flatBenchSubject struct {
	Email    string `json:"email"    validation:"required|email"`
	Password string `json:"password" validation:"required|password"`
	Age      int    `json:"age"      validation:"required|min:18|max:120"`
	Website  string `json:"website"  validation:"url"`
}

type nestedBenchItem struct {
	Name  string `json:"name"  validation:"required"`
	Label string `json:"label" validation:"required|min:1|max:50"`
}

type nestedBenchSubject struct {
	Title string            `json:"title" validation:"required|min:3|max:100"`
	Items []nestedBenchItem `json:"items"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Benchmarks
// ─────────────────────────────────────────────────────────────────────────────

// BenchmarkValidate_Flat_Valid measures the steady-state cost of a successful
// flat-struct validation (cache warmed after the first iteration).
func BenchmarkValidate_Flat_Valid(b *testing.B) {
	v := validation.New()
	req := &flatBenchSubject{
		Email:    "alice@example.com",
		Password: "Str0ng!Pass",
		Age:      25,
		Website:  "https://example.com",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Validate(req)
	}
}

// BenchmarkValidate_Flat_Invalid measures the steady-state cost when several
// fields fail validation (error path allocations are included).
func BenchmarkValidate_Flat_Invalid(b *testing.B) {
	v := validation.New()
	req := &flatBenchSubject{
		Email:    "not-an-email",
		Password: "weak",
		Age:      10,
		Website:  "not-a-url",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Validate(req)
	}
}

// BenchmarkValidate_Nested_Valid measures validation of a struct containing a
// slice of nested structs (10 elements, all valid).
func BenchmarkValidate_Nested_Valid(b *testing.B) {
	v := validation.New()
	items := make([]nestedBenchItem, 10)
	for i := range items {
		items[i] = nestedBenchItem{Name: "item", Label: "label"}
	}
	req := &nestedBenchSubject{Title: "my title", Items: items}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Validate(req)
	}
}

// BenchmarkValidate_Nested_Invalid is the same as Valid but with failing fields.
func BenchmarkValidate_Nested_Invalid(b *testing.B) {
	v := validation.New()
	items := make([]nestedBenchItem, 10)
	for i := range items {
		items[i] = nestedBenchItem{Name: "", Label: ""}
	}
	req := &nestedBenchSubject{Title: "", Items: items}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Validate(req)
	}
}
