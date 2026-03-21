package product

// CreateProductRequest is the validated input for creating a product.
// Tags drive both binders (JSON parsing + stateless validation) and
// the validation package (stateful rules like "exists").
type CreateProductRequest struct {
	Name        string  `form:"name"         json:"name"         validation:"required|max:150"`
	SKU         string  `form:"sku"          json:"sku"          validation:"required|max:50"`
	Description string  `form:"description"  json:"description"  validation:"max:1000"`
	Price       float64 `form:"price"        json:"price"        validation:"required|min:0"`
	Stock       int     `form:"stock"        json:"stock"        validation:"min:0"`
	CategoryID  int     `form:"category_id"  json:"category_id"`
}

// UpdateProductRequest allows partial updates.
// Fields absent from the JSON body are left at their zero value and skipped.
type UpdateProductRequest struct {
	Name        string  `form:"name"         json:"name"         validation:"max:150"`
	SKU         string  `form:"sku"          json:"sku"          validation:"max:50"`
	Description string  `form:"description"  json:"description"  validation:"max:1000"`
	Price       float64 `form:"price"        json:"price"        validation:"min:0"`
	Stock       int     `form:"stock"        json:"stock"        validation:"min:0"`
	CategoryID  int     `form:"category_id"  json:"category_id"`
}
