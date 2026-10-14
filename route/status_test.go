package route_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wssto2/go-core/route"
)

func TestStatusCreatedAnswers201WithTheEnvelope(t *testing.T) {
	r := route.Post[route.None, string]("/x").Status(http.StatusCreated).To(func(context.Context, route.None) (string, error) {
		return "made", nil
	})

	e := newEngine()
	if err := r.Mount(e, open()); err != nil {
		t.Fatal(err)
	}

	rec := do(e, http.MethodPost, "/x", "")
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"data":"made"`) || r.Spec().Status != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestStatusNoContentAnswersNoBody(t *testing.T) {
	r := route.Delete[route.None, string]("/x").Status(http.StatusNoContent).To(func(context.Context, route.None) (string, error) {
		return "ignored", nil
	})

	e := newEngine()
	_ = r.Mount(e, open())

	if rec := do(e, http.MethodDelete, "/x", ""); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status %d: %q", rec.Code, rec.Body)
	}
}

func TestStatusRefusesNon2xx(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusBadRequest, http.StatusInternalServerError, 0} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("Status(%d) must panic", status)
				}
			}()
			route.Get[route.None, string]("/x").Status(status)
		}()
	}
}

type ratesInput struct {
	Rate float64 `json:"rate" validation:"gte=0"`
}

func TestAnUnknownValidationRuleIsRefusedWhenMounted(t *testing.T) {
	r := route.Post[ratesInput, route.Empty]("/rates").Name("rates.save").To(func(context.Context, ratesInput) (route.Empty, error) {
		return route.Empty{}, nil
	})

	err := r.Mount(newEngine(), open())
	if err == nil || !strings.Contains(err.Error(), "ratesInput.Rate") || !strings.Contains(err.Error(), `"gte=0"`) || !strings.Contains(err.Error(), "rates.save") {
		t.Fatalf("got %v", err)
	}
}
