package product

// CreateProductRequest is the validated input for creating a product.
// Tags drive both binders (JSON parsing + stateless validation) and
// the validation package (stateful rules like "exists").
type CreateProductRequest struct {
	Name        string  `form:"name"         json:"name"`
	SKU         string  `form:"sku"          json:"sku"`
	Description string  `form:"description"  json:"description"`
	Price       float64 `form:"price"        json:"price"`
	Stock       int     `form:"stock"        json:"stock"`
	CategoryID  int     `form:"category_id"  json:"category_id"`
}

// ToInput maps the parsed HTTP request into the domain input struct.
// The returned input is not yet validated — call input.Validate() or
// pass through the service which validates on entry.
func (r CreateProductRequest) ToInput() CreateProductOptions {
	return CreateProductOptions{
		Name:        r.Name,
		SKU:         r.SKU,
		Description: r.Description,
		Price:       r.Price,
		Stock:       r.Stock,
		CategoryID:  r.CategoryID,
	}
}

// UpdateProductRequest allows partial updates.
// Fields absent from the JSON body are left at their zero value and skipped.
type UpdateProductRequest struct {
	Name        string  `form:"name"         json:"name"`
	SKU         string  `form:"sku"          json:"sku"`
	Description string  `form:"description"  json:"description"`
	Price       float64 `form:"price"        json:"price"`
	Stock       int     `form:"stock"        json:"stock"`
	CategoryID  int     `form:"category_id"  json:"category_id"`
}

func (r UpdateProductRequest) ToInput() UpdateProductOptions {
	return UpdateProductOptions{
		Name:        r.Name,
		SKU:         r.SKU,
		Description: r.Description,
		Price:       r.Price,
		Stock:       r.Stock,
		CategoryID:  r.CategoryID,
	}
}
