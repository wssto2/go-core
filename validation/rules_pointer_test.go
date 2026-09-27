package validation

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Optional input fields are pointers (*int64, *string, …). The size rules must
// check the value a non-nil pointer points to; a nil pointer is "not provided"
// and is left to `required`.

func int64Ptr(v int64) *int64 { return &v }

func strPtr(v string) *string { return &v }

func TestMaxRule_PointerInt(t *testing.T) {
	t.Run("above the bound fails", func(t *testing.T) {
		failures := collectFailures(MaxRule, int64Ptr(101), "100", false)
		if assert.Len(t, failures, 1) {
			assert.Equal(t, CodeMax, failures[0].Code)
		}
	})
	t.Run("at the bound passes", func(t *testing.T) {
		assert.Empty(t, collectFailures(MaxRule, int64Ptr(100), "100", false))
	})
	t.Run("below the bound passes", func(t *testing.T) {
		assert.Empty(t, collectFailures(MaxRule, int64Ptr(99), "100", false))
	})
	t.Run("nil is not provided", func(t *testing.T) {
		var value *int64
		assert.Empty(t, collectFailures(MaxRule, value, "100", false))
		assert.Empty(t, collectFailures(MaxRule, value, "100", true))
	})
	t.Run("*int above the bound fails", func(t *testing.T) {
		v := 7
		assert.Len(t, collectFailures(MaxRule, &v, "5", false), 1)
	})
	t.Run("*float64 above the bound fails", func(t *testing.T) {
		v := 5.5
		assert.Len(t, collectFailures(MaxRule, &v, "5", false), 1)
	})
}

func TestMinRule_PointerInt(t *testing.T) {
	t.Run("below the bound fails", func(t *testing.T) {
		failures := collectFailures(MinRule, int64Ptr(9), "10", false)
		if assert.Len(t, failures, 1) {
			assert.Equal(t, CodeMin, failures[0].Code)
		}
	})
	t.Run("at the bound passes", func(t *testing.T) {
		assert.Empty(t, collectFailures(MinRule, int64Ptr(10), "10", false))
	})
	t.Run("above the bound passes", func(t *testing.T) {
		assert.Empty(t, collectFailures(MinRule, int64Ptr(11), "10", false))
	})
	t.Run("pointer to zero below the bound fails", func(t *testing.T) {
		assert.Len(t, collectFailures(MinRule, int64Ptr(0), "1", false), 1)
	})
	t.Run("nil is not provided", func(t *testing.T) {
		var value *int64
		assert.Empty(t, collectFailures(MinRule, value, "10", false))
		assert.Empty(t, collectFailures(MinRule, value, "10", true))
	})
}

func TestBetweenRule_PointerInt(t *testing.T) {
	assert.Len(t, collectFailures(BetweenRule, int64Ptr(11), "1,10", false), 1)
	assert.Len(t, collectFailures(BetweenRule, int64Ptr(0), "1,10", false), 1)
	assert.Empty(t, collectFailures(BetweenRule, int64Ptr(10), "1,10", false))
	var value *int64
	assert.Empty(t, collectFailures(BetweenRule, value, "1,10", false))
}

func TestSizeRules_PointerString(t *testing.T) {
	assert.Len(t, collectFailures(MaxRule, strPtr("abcdef"), "5", false), 1)
	assert.Empty(t, collectFailures(MaxRule, strPtr("abcde"), "5", false))
	assert.Len(t, collectFailures(MinRule, strPtr("ab"), "3", false), 1)
	assert.Empty(t, collectFailures(LenRule, strPtr("abc"), "3", false))
	assert.Len(t, collectFailures(LenRule, strPtr("ab"), "3", false), 1)
}

func TestValidate_MaxOnOptionalPointerFields(t *testing.T) {
	type Input struct {
		Mileage *int64  `json:"mileage" validation:"max:100"`
		Note    *string `json:"note"    validation:"max:5"`
	}

	err := New().Validate(&Input{Mileage: int64Ptr(101), Note: strPtr("abcdef")})
	assert.Error(t, err)
	var ve *ValidationError
	assert.True(t, errors.As(err, &ve))
	assert.Contains(t, ve.Failures, "mileage")
	assert.Contains(t, ve.Failures, "note")

	assert.NoError(t, New().Validate(&Input{Mileage: int64Ptr(100), Note: strPtr("abcde")}))
	assert.NoError(t, New().Validate(&Input{}))
}
