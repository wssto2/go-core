package product

import (
	"github.com/wssto2/go-core/validation"
)

type CreateProductOptions struct {
	Name        string  `validation:"required|max:150"`
	SKU         string  `validation:"required|max:50"`
	Description string  `validation:"max:1000"`
	Price       float64 `validation:"required|min:0"`
	Stock       int     `validation:"min:0"`
	CategoryID  int     `validation:""`
}

func (i CreateProductOptions) Validate() error {
	return validation.ValidateInput(&i)
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
	return validation.ValidateInput(&i)
}
