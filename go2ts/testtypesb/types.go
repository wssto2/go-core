package testtypesb

type Pricing struct {
	Gross float64 `json:"gross"`
}

type RequestPricing struct {
	Rate int `json:"rate" validation:"required|min:1"`
}
