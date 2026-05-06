package testtypesa

type Pricing struct {
	Net float64 `json:"net"`
}

type RequestPricing struct {
	Amount float64 `json:"amount" validation:"required|min:0"`
}
