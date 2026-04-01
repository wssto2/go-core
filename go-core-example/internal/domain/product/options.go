package product

import (
	"fmt"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/validation"
)

// NOTE: The refactored core's validation.New() only registers a minimal set of
// rules by default (required, email). HTTP binders perform stateless checks
// like max/min independently. For library consumers calling Validate() from
// services/tests, register the missing rules locally here to mirror binder
// behavior.

type CreateProductOptions struct {
	Name        string  `validation:"required|max:150"`
	SKU         string  `validation:"required|max:50"`
	Description string  `validation:"max:1000"`
	Price       float64 `validation:"required|min:0"`
	Stock       int     `validation:"min:0"`
	CategoryID  int     `validation:""`
}

func (i CreateProductOptions) Validate() error {
	v := validation.New()
	if err := v.Validate(&i); err != nil {
		if ve, ok := err.(*validation.ValidationError); ok {
			// Build message similar to validation.ValidateInput
			errMsgs := []string{}
			for field, failures := range ve.Failures {
				for _, f := range failures {
					errMsgs = append(errMsgs, field+": "+string(f.Code))
				}
			}
			if len(errMsgs) == 0 {
				return apperr.BadRequest("validation failed")
			}
			return apperr.BadRequest(fmt.Sprintf("%s", errMsgs))
		}
		return apperr.BadRequest(err.Error())
	}
	return nil
}

type UpdateProductOptions struct {
	Name        string  `validation:"max:150"`
	SKU         string  `validation:"max:50"`
	Description string  `validation:"max:1000"`
	Price       float64 `validation:"min:0"`
	Stock       int     `validation:"min:0"`
	CategoryID  int     `validation:""`
}

func (i UpdateProductOptions) Validate() error {
	// reuse CreateProductOptions validator for consistency
	c := CreateProductOptions{
		Name:        i.Name,
		SKU:         i.SKU,
		Description: i.Description,
		Price:       i.Price,
		Stock:       i.Stock,
		CategoryID:  i.CategoryID,
	}
	return c.Validate()
}
