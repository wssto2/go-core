package validation_test

import (
	"strings"
	"testing"

	"github.com/wssto2/go-core/validation"
)

type embeddedRates struct {
	Rate float64 `json:"rate" validation:"gte=0"`
}

type okInput struct {
	Name string `json:"name" validation:"required|max:10|min:2"`
	When string `json:"when" validation:"date"`
}

type badOuter struct {
	Name  string          `json:"name" validation:"required"`
	Items []embeddedRates `json:"items"`
}

type badEmbedding struct {
	okInput
	embeddedRates
}

func TestCheckRulesRefusesAnUnknownRuleAndNamesIt(t *testing.T) {
	for _, v := range []any{embeddedRates{}, badOuter{}, &badOuter{}, badEmbedding{}} {
		err := validation.CheckRules(v)
		if err == nil || !strings.Contains(err.Error(), "embeddedRates.Rate") && !strings.Contains(err.Error(), "badEmbedding") || !strings.Contains(err.Error(), `"gte=0"`) {
			t.Fatalf("%T: got %v", v, err)
		}
	}
}

func TestCheckRulesAcceptsKnownRules(t *testing.T) {
	if err := validation.CheckRules(okInput{}); err != nil {
		t.Fatal(err)
	}
}
